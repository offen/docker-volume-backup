// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestHostKeyAlgorithms(t *testing.T) {
	tests := []struct {
		keyType string
		want    []string
	}{
		{gossh.KeyAlgoED25519, []string{gossh.KeyAlgoED25519}},
		{gossh.KeyAlgoECDSA521, []string{gossh.KeyAlgoECDSA521}},
		{gossh.KeyAlgoRSA, []string{gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSA}},
	}
	for _, test := range tests {
		t.Run(test.keyType, func(t *testing.T) {
			if got := hostKeyAlgorithms(test.keyType); !slices.Equal(got, test.want) {
				t.Fatalf("hostKeyAlgorithms(%q) = %v; want %v", test.keyType, got, test.want)
			}
		})
	}
}

func TestKnownHostKeyAlgorithms(t *testing.T) {
	ed25519Key, ecdsaKey := generateHostKeys(t)
	file := writeKnownHosts(t, "127.0.0.1:2222", ed25519Key.PublicKey(), ecdsaKey.PublicKey())
	cb, err := knownhosts.New(file)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{gossh.KeyAlgoED25519, gossh.KeyAlgoECDSA256}
	if got := knownHostKeyAlgorithms(cb, "127.0.0.1", "2222"); !slices.Equal(got, want) {
		t.Fatalf("known host = %v; want %v", got, want)
	}
	if got := knownHostKeyAlgorithms(cb, "127.0.0.1", "22"); got != nil {
		t.Fatalf("unknown host = %v; want nil", got)
	}
}

// The server offers ED25519 and ECDSA host keys. Without restricting the host
// key algorithms, the client negotiates ECDSA and a pinned ED25519 key fails.
func TestNewStorageBackendVerifiesPinnedHostKeyOfAnyType(t *testing.T) {
	ed25519Key, ecdsaKey := generateHostKeys(t)
	address := startSFTPServer(t, ed25519Key, ecdsaKey)
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, _ := generateHostKeys(t)

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{name: "public key ed25519", config: Config{HostPublicKey: authorizedKey(ed25519Key)}},
		{name: "public key ecdsa", config: Config{HostPublicKey: authorizedKey(ecdsaKey)}},
		{name: "public key mismatch", config: Config{HostPublicKey: authorizedKey(otherKey)}, wantErr: true},
		{name: "known hosts ed25519", config: Config{KnownHostsFile: writeKnownHosts(t, address, ed25519Key.PublicKey())}},
		{name: "known hosts ecdsa", config: Config{KnownHostsFile: writeKnownHosts(t, address, ecdsaKey.PublicKey())}},
		{name: "known hosts mismatch", config: Config{KnownHostsFile: writeKnownHosts(t, address, otherKey.PublicKey())}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := test.config
			config.HostName, config.Port, config.User, config.RemotePath = host, port, "test", "/backups"
			_, cleanup, err := NewStorageBackend(config, func(storage.LogLevel, string, string, ...any) {})
			t.Cleanup(func() { _ = cleanup() })
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "mismatch") {
					t.Fatalf("expected host key mismatch, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected connection to succeed, got %v", err)
			}
		})
	}
}

func generateHostKeys(t *testing.T) (gossh.Signer, gossh.Signer) {
	t.Helper()
	_, ed25519Private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ed25519Signer, err := gossh.NewSignerFromKey(ed25519Private)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaPrivate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaSigner, err := gossh.NewSignerFromKey(ecdsaPrivate)
	if err != nil {
		t.Fatal(err)
	}
	return ed25519Signer, ecdsaSigner
}

func authorizedKey(signer gossh.Signer) string {
	return string(gossh.MarshalAuthorizedKey(signer.PublicKey()))
}

func writeKnownHosts(t *testing.T, address string, keys ...gossh.PublicKey) string {
	t.Helper()
	var lines []string
	for _, key := range keys {
		lines = append(lines, knownhosts.Line([]string{knownhosts.Normalize(address)}, key))
	}
	file := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

// startSFTPServer starts an in-memory SFTP server presenting the given host keys.
func startSFTPServer(t *testing.T, hostKeys ...gossh.Signer) string {
	t.Helper()
	config := &gossh.ServerConfig{NoClientAuth: true}
	for _, hostKey := range hostKeys {
		config.AddHostKey(hostKey)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	t.Cleanup(func() {
		_ = listener.Close()
		workers.Wait()
	})
	workers.Go(func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() { serveSFTP(connection, config) })
		}
	})
	return listener.Addr().String()
}

func serveSFTP(connection net.Conn, config *gossh.ServerConfig) {
	defer func() { _ = connection.Close() }()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
	server, channels, requests, err := gossh.NewServerConn(connection, config)
	if err != nil {
		return
	}
	defer func() { _ = server.Close() }()
	go gossh.DiscardRequests(requests)
	for incoming := range channels {
		if incoming.ChannelType() != "session" {
			_ = incoming.Reject(gossh.UnknownChannelType, "expected session")
			continue
		}
		channel, requests, err := incoming.Accept()
		if err != nil {
			return
		}
		go func() {
			for request := range requests {
				_ = request.Reply(request.Type == "subsystem", nil)
			}
		}()
		requestServer := sftp.NewRequestServer(channel, sftp.InMemHandler())
		_ = requestServer.Serve()
		_ = requestServer.Close()
	}
}
