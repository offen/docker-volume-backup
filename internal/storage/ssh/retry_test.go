// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/pkg/sftp"
)

type uploadTestClient struct {
	mkdirErr error
	create   func(string) (io.WriteCloser, error)
}

func (c *uploadTestClient) MkdirAll(string) error { return c.mkdirErr }
func (c *uploadTestClient) Create(name string) (io.WriteCloser, error) {
	return c.create(name)
}
func (c *uploadTestClient) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }
func (c *uploadTestClient) Remove(string) error                   { return nil }

type uploadTestFile struct {
	write func([]byte) (int, error)
	close func() error
}

func (f *uploadTestFile) Write(p []byte) (int, error) { return f.write(p) }
func (f *uploadTestFile) Close() error                { return f.close() }

func uploadTestSource(t *testing.T) (string, []byte) {
	t.Helper()
	data := bytes.Repeat([]byte("a completed, encrypted backup\n"), 4096)
	name := filepath.Join(t.TempDir(), "backup.tar.gz.age")
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return name, data
}

func uploadTestBackend(client sftpClient, retries int, logs *[]string) *sshStorage {
	return &sshStorage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: "/backups",
			Log: func(level storage.LogLevel, _ string, message string, args ...any) {
				if level == storage.LogLevelInfo {
					*logs = append(*logs, fmt.Sprintf(message, args...))
				}
			},
		},
		sftpClient:    client,
		closeSession:  noop,
		uploadRetries: retries,
	}
}

func TestCopyReconnectsAndRestartsUpload(t *testing.T) {
	file, data := uploadTestSource(t)
	synctest.Test(t, func(t *testing.T) {
		var remote bytes.Buffer
		var logs []string
		var connections, creates, oldCloses, newCloses, fileCloses int
		first := &uploadTestClient{create: func(name string) (io.WriteCloser, error) {
			if name != "/backups/backup.tar.gz.age" {
				t.Fatalf("Destination = %q", name)
			}
			creates++
			remote.Reset()
			return &uploadTestFile{
				write: func(p []byte) (int, error) {
					n, _ := remote.Write(p[:13])
					return n, sftp.ErrSSHFxConnectionLost
				},
				close: func() error { fileCloses++; return io.EOF },
			}, nil
		}}
		b := uploadTestBackend(first, 2, &logs)
		b.uploadRetryDelay = 5 * time.Second
		b.closeSession = func() error {
			oldCloses++
			return errors.New("old transport already closed")
		}
		started := time.Now()
		b.connect = func() (sftpClient, func() error, error) {
			connections++
			if oldCloses != 1 || fileCloses != 1 {
				t.Fatalf("Reconnect before closing old session/file: %d/%d", oldCloses, fileCloses)
			}
			if elapsed := time.Since(started); elapsed != b.uploadRetryDelay {
				t.Fatalf("Retry after %s, want %s", elapsed, b.uploadRetryDelay)
			}
			client := &uploadTestClient{create: func(string) (io.WriteCloser, error) {
				creates++
				remote.Reset()
				return &uploadTestFile{
					write: remote.Write,
					close: func() error {
						if len(logs) != 0 {
							t.Error("Success logged before closing the remote file")
						}
						fileCloses++
						return nil
					},
				}, nil
			}}
			return client, func() error { newCloses++; return nil }, nil
		}
		if err := b.Copy(file); err != nil {
			t.Fatalf("Copy: %v", err)
		}
		if !bytes.Equal(remote.Bytes(), data) {
			t.Fatal("Retry did not upload the complete source from byte zero")
		}
		if connections != 1 || creates != 2 || fileCloses != 2 || len(logs) != 1 {
			t.Fatalf("connections=%d, creates=%d, fileCloses=%d, successLogs=%d", connections, creates, fileCloses, len(logs))
		}
		if err := b.close(); err != nil {
			t.Fatalf("Cleanup retained the old session's close error: %v", err)
		}
		if err := b.close(); err != nil || newCloses != 1 {
			t.Fatalf("Repeated cleanup: err=%v, newCloses=%d", err, newCloses)
		}
	})
}

func TestCopyRetriesRemoteCloseFailure(t *testing.T) {
	file, data := uploadTestSource(t)
	var logs []string
	var uploads int
	client := func(closeErr error) *uploadTestClient {
		return &uploadTestClient{create: func(string) (io.WriteCloser, error) {
			uploads++
			var received bytes.Buffer
			return &uploadTestFile{
				write: received.Write,
				close: func() error {
					if !bytes.Equal(received.Bytes(), data) {
						t.Error("Upload differs from source")
					}
					if len(logs) != 0 {
						t.Error("Success logged before remote close")
					}
					return closeErr
				},
			}, nil
		}}
	}
	b := uploadTestBackend(client(io.EOF), 1, &logs)
	b.connect = func() (sftpClient, func() error, error) { return client(nil), noop, nil }
	if err := b.Copy(file); err != nil {
		t.Fatal(err)
	}
	if uploads != 2 || len(logs) != 1 {
		t.Fatalf("uploads=%d, successLogs=%d; want 2, 1", uploads, len(logs))
	}
}

func TestCopyRetryLimit(t *testing.T) {
	file, _ := uploadTestSource(t)
	for _, reconnectFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("reconnectFails=%v", reconnectFails), func(t *testing.T) {
			var logs []string
			var connections, closes int
			client := &uploadTestClient{mkdirErr: sftp.ErrSSHFxConnectionLost}
			b := uploadTestBackend(client, 2, &logs)
			b.closeSession = func() error { closes++; return nil }
			b.connect = func() (sftpClient, func() error, error) {
				connections++
				if reconnectFails {
					return nil, noop, io.ErrUnexpectedEOF
				}
				return client, func() error { closes++; return nil }, nil
			}
			if err := b.Copy(file); err == nil {
				t.Fatal("Copy succeeded after exhausting retries")
			}
			if connections != 2 || len(logs) != 0 {
				t.Fatalf("connections=%d, successLogs=%d; want 2, 0", connections, len(logs))
			}
			if err := b.close(); err != nil {
				t.Fatal(err)
			}
			wantCloses := 3
			if reconnectFails {
				wantCloses = 1
			}
			if closes != wantCloses {
				t.Fatalf("Closed %d sessions, want %d", closes, wantCloses)
			}
		})
	}
}

func TestCopyDoesNotRetryPermanentErrorsOrWhenDisabled(t *testing.T) {
	file, _ := uploadTestSource(t)
	for _, tt := range []struct {
		name    string
		err     error
		retries int
	}{
		{"disabled", sftp.ErrSSHFxConnectionLost, 0},
		{"permission", os.ErrPermission, 2},
		{"remote failure", sftp.ErrSSHFxFailure, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs []string
			b := uploadTestBackend(&uploadTestClient{mkdirErr: tt.err}, tt.retries, &logs)
			b.connect = func() (sftpClient, func() error, error) {
				t.Fatal("Unexpected retry")
				return nil, noop, nil
			}
			if err := b.Copy(file); !errors.Is(err, tt.err) {
				t.Fatalf("Copy error = %v, want %v", err, tt.err)
			}
			if len(logs) != 0 {
				t.Fatal("Success logged for a failed upload")
			}
		})
	}
}

func TestCopyDoesNotRetryMissingSource(t *testing.T) {
	var logs []string
	b := uploadTestBackend(nil, 2, &logs)
	if err := b.Copy(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Missing source: %v", err)
	}
}

func TestCopyDoesNotRetryPermanentWriteErrorWithFailedClose(t *testing.T) {
	file, _ := uploadTestSource(t)
	var logs []string
	var closes int
	client := &uploadTestClient{create: func(string) (io.WriteCloser, error) {
		return &uploadTestFile{
			write: func([]byte) (int, error) { return 0, os.ErrPermission },
			close: func() error { closes++; return io.EOF },
		}, nil
	}}
	b := uploadTestBackend(client, 2, &logs)
	b.connect = func() (sftpClient, func() error, error) {
		t.Fatal("A close error caused a permanent write error to be retried")
		return nil, noop, nil
	}
	err := b.Copy(file)
	if !errors.Is(err, os.ErrPermission) || !errors.Is(err, io.EOF) {
		t.Fatalf("Copy error = %v, want permission and close errors", err)
	}
	if closes != 1 || len(logs) != 0 {
		t.Fatalf("closes=%d, successLogs=%d; want 1, 0", closes, len(logs))
	}
}

func TestCopyFileChecksSizeAndClosesRemoteFile(t *testing.T) {
	file, data := uploadTestSource(t)
	source, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var logs []string
	var closes int
	client := &uploadTestClient{create: func(string) (io.WriteCloser, error) {
		return &uploadTestFile{
			write: io.Discard.Write,
			close: func() error { closes++; return nil },
		}, nil
	}}
	b := uploadTestBackend(client, 2, &logs)
	err = b.copyFile(source, int64(len(data))+1, filepath.Base(file))
	if err == nil || !strings.Contains(err.Error(), "failed to upload the file completely") {
		t.Fatalf("Size mismatch error = %v", err)
	}
	if retryableUploadError(err) || closes != 1 {
		t.Fatalf("Size mismatch retryable=%v, closes=%d", retryableUploadError(err), closes)
	}
}

func TestRetryableUploadError(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"EOF", io.EOF, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"closed pipe", io.ErrClosedPipe, true},
		{"closed network", net.ErrClosed, true},
		{"network operation", &net.OpError{Op: "write", Net: "tcp", Err: errors.New("reset")}, true},
		{"SFTP lost", sftp.ErrSSHFxConnectionLost, true},
		{"SFTP disconnected", sftp.ErrSSHFxNoConnection, true},
		{"SFTP status lost", &sftp.StatusError{Code: uint32(sftp.ErrSSHFxConnectionLost)}, true},
		{"SFTP status permission", &sftp.StatusError{Code: uint32(sftp.ErrSSHFxPermissionDenied)}, false},
		{"SFTP no space", &sftp.StatusError{Code: 14}, false},
		{"SFTP failure", sftp.ErrSSHFxFailure, false},
		{"permission", os.ErrPermission, false},
		{"local read", &os.PathError{Op: "read", Path: "backup", Err: io.ErrUnexpectedEOF}, false},
		{"unrecognized", errors.New("connection lost"), false},
		{"joined transport", errors.Join(io.EOF, sftp.ErrSSHFxConnectionLost), true},
		{"joined permanent", errors.Join(os.ErrPermission, io.EOF), false},
		{"wrapped joined permanent", fmt.Errorf("upload: %w", errors.Join(os.ErrPermission, io.EOF)), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryableUploadError(tt.err); got != tt.want {
				t.Fatalf("retryableUploadError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNewStorageBackendRejectsNegativeRetrySettings(t *testing.T) {
	for _, cfg := range []Config{{UploadRetries: -1}, {UploadRetryDelay: -time.Second}} {
		_, cleanup, err := NewStorageBackend(cfg, nil)
		if err == nil || !strings.Contains(err.Error(), "must not be negative") {
			t.Fatalf("Config %+v: %v", cfg, err)
		}
		if err := cleanup(); err != nil {
			t.Fatal(err)
		}
	}
}
