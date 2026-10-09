// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// Exercise the real SSH/SFTP clients: the first connection is severed after
// receiving part of the archive, then the retry must upload every byte again.
func TestCopyReconnectsAfterInterruptedSFTPUpload(t *testing.T) {
	handlers := sftp.InMemHandler()
	address, accepted, disconnected := startInterruptingSFTPServer(t, handlers)
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	contents := bytes.Repeat([]byte("encrypted-backup-content\n"), 32768)
	file := filepath.Join(t.TempDir(), "backup.tar.gz.age")
	if err := os.WriteFile(file, contents, 0600); err != nil {
		t.Fatal(err)
	}
	var successes, warnings int
	backend, cleanup, err := NewStorageBackend(Config{
		HostName: host, Port: port, User: "test", RemotePath: "/backups",
		UploadRetries: 2, UploadRetryDelay: time.Millisecond,
	}, func(level storage.LogLevel, _ string, message string, _ ...any) {
		if level == storage.LogLevelInfo && strings.HasPrefix(message, "Uploaded a copy") {
			successes++
		}
		if level == storage.LogLevelWarning {
			warnings++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup() })
	// Production uses Unix paths; normalize the temporary Windows path as well.
	if err := backend.Copy(filepath.ToSlash(file)); err != nil {
		t.Fatalf("Copy did not recover from the interrupted upload: %v", err)
	}
	if accepted.Load() != 2 || disconnected.Load() != 1 {
		t.Fatalf("connections=%d, injected disconnects=%d; want 2, 1", accepted.Load(), disconnected.Load())
	}
	if successes != 1 || warnings != 1 {
		t.Fatalf("success logs=%d, retry warnings=%d; want 1 each", successes, warnings)
	}
	request := sftp.NewRequest("Get", "/backups/backup.tar.gz.age")
	request.Flags = 1 // SSH_FXF_READ
	remote, err := handlers.FileGet.Fileread(request)
	if err != nil {
		t.Fatal(err)
	}
	if closer, ok := remote.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}
	actual, err := io.ReadAll(io.NewSectionReader(remote, 0, int64(len(contents)+1)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, contents) {
		t.Fatalf("remote archive differs from source: got %d bytes, want %d", len(actual), len(contents))
	}
	// Pruning must also use the replacement session, not the failed one.
	if _, err := backend.Prune(time.Now().Add(-time.Hour), "backup-"); err != nil {
		t.Fatalf("Prune after reconnect: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("Cleanup reported the discarded connection's error: %v", err)
	}
}

type interruptedSFTPWriter struct {
	sftp.FileWriter
	connection   net.Conn
	disconnected *atomic.Int32
}

func (w *interruptedSFTPWriter) OpenFile(request *sftp.Request) (sftp.WriterAtReaderAt, error) {
	file, err := w.FileWriter.(sftp.OpenFileWriter).OpenFile(request)
	if err != nil {
		return nil, err
	}
	return &interruptedSFTPFile{WriterAtReaderAt: file, writer: w}, nil
}

type interruptedSFTPFile struct {
	sftp.WriterAtReaderAt
	writer *interruptedSFTPWriter
}

func (f *interruptedSFTPFile) WriteAt(p []byte, offset int64) (int, error) {
	if offset >= 32*1024 {
		if f.writer.disconnected.CompareAndSwap(0, 1) {
			_ = f.writer.connection.Close()
		}
		return 0, io.ErrClosedPipe
	}
	return f.WriterAtReaderAt.WriteAt(p, offset)
}

func (f *interruptedSFTPFile) Close() error {
	if closer, ok := f.WriterAtReaderAt.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func startInterruptingSFTPServer(t *testing.T, handlers sftp.Handlers) (string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	config := &gossh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted, disconnected atomic.Int32
	var connections []net.Conn
	var mu sync.Mutex
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		mu.Lock()
		for _, connection := range connections {
			_ = connection.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	go func() {
		defer close(acceptDone)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, connection)
			mu.Unlock()
			number := accepted.Add(1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { _ = connection.Close() }()
				_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
				server, channels, requests, err := gossh.NewServerConn(connection, config)
				if err != nil {
					return
				}
				defer func() { _ = server.Close() }()
				workers.Go(func() { gossh.DiscardRequests(requests) })
				for incoming := range channels {
					if incoming.ChannelType() != "session" {
						_ = incoming.Reject(gossh.UnknownChannelType, "expected session")
						continue
					}
					channel, requests, err := incoming.Accept()
					if err != nil {
						return
					}
					workers.Go(func() {
						defer func() { _ = channel.Close() }()
						for request := range requests {
							var subsystem struct{ Name string }
							valid := request.Type == "subsystem" && gossh.Unmarshal(request.Payload, &subsystem) == nil && subsystem.Name == "sftp"
							if err := request.Reply(valid, nil); err != nil || !valid {
								continue
							}
							sessionHandlers := handlers
							if number == 1 {
								sessionHandlers.FilePut = &interruptedSFTPWriter{FileWriter: handlers.FilePut, connection: connection, disconnected: &disconnected}
							}
							sftpServer := sftp.NewRequestServer(channel, sessionHandlers)
							_ = sftpServer.Serve()
							_ = sftpServer.Close()
							return
						}
					})
				}
			}()
		}
	}()
	return listener.Addr().String(), &accepted, &disconnected
}
