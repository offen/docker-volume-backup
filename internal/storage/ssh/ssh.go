// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/offen/docker-volume-backup/internal/errwrap"
	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sshStorage struct {
	*storage.StorageBackend
	sftpClient       sftpClient
	connect          func() (sftpClient, func() error, error)
	closeSession     func() error
	hostName         string
	uploadRetries    int
	uploadRetryDelay time.Duration
}

type sftpClient interface {
	MkdirAll(string) error
	Create(string) (io.WriteCloser, error)
	ReadDir(string) ([]os.FileInfo, error)
	Remove(string) error
}

type sftpConnection struct{ *sftp.Client }

func (c sftpConnection) Create(name string) (io.WriteCloser, error) {
	return c.Client.Create(name)
}

// Config allows to configure a SSH backend.
type Config struct {
	HostName           string
	Port               string
	User               string
	Password           string
	IdentityFile       string
	IdentityPassphrase string
	RemotePath         string
	UploadRetries      int
	UploadRetryDelay   time.Duration
}

var noop = func() error { return nil }

// NewStorageBackend creates and initializes a new SSH storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, func() error, error) {
	if opts.UploadRetries < 0 || opts.UploadRetryDelay < 0 {
		return nil, noop, errwrap.Wrap(nil, "SSH upload retries and retry delay must not be negative")
	}
	var authMethods []ssh.AuthMethod

	if opts.Password != "" {
		authMethods = append(authMethods, ssh.Password(opts.Password))
	}

	if _, err := os.Stat(opts.IdentityFile); err == nil {
		key, err := os.ReadFile(opts.IdentityFile)
		if err != nil {
			return nil, noop, errwrap.Wrap(nil, "error reading the private key")
		}

		var signer ssh.Signer
		if opts.IdentityPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(opts.IdentityPassphrase))
			if err != nil {
				return nil, noop, errwrap.Wrap(nil, "error parsing the encrypted private key")
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
			if err != nil {
				return nil, noop, errwrap.Wrap(nil, "error parsing the private key")
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	sshClientConfig := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	connect := func() (sftpClient, func() error, error) {
		client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", opts.HostName, opts.Port), sshClientConfig)
		if err != nil {
			return nil, noop, errwrap.Wrap(err, "error creating ssh client")
		}
		if _, _, err := client.SendRequest("keepalive", false, nil); err != nil {
			_ = client.Close()
			return nil, noop, err
		}

		sftpClient, err := sftp.NewClient(client,
			sftp.UseConcurrentReads(true),
			sftp.UseConcurrentWrites(true),
			sftp.MaxConcurrentRequestsPerFile(64),
		)
		if err != nil {
			_ = client.Close()
			return nil, noop, errwrap.Wrap(err, "error creating sftp client")
		}
		return sftpConnection{sftpClient}, startKeepAlive(client, keepAliveInterval), nil
	}

	b := &sshStorage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.RemotePath,
			Log:             logFunc,
		},
		connect:          connect,
		hostName:         opts.HostName,
		uploadRetries:    opts.UploadRetries,
		uploadRetryDelay: opts.UploadRetryDelay,
	}
	var err error
	b.sftpClient, b.closeSession, err = connect()
	if err != nil {
		return nil, noop, err
	}
	// Resolve the current session at cleanup time, as Copy can replace it.
	return b, sync.OnceValue(b.close), nil
}

func (b *sshStorage) close() error {
	if b.closeSession == nil {
		return nil
	}
	closeSession := b.closeSession
	b.closeSession = nil
	return closeSession()
}

// Name returns the name of the storage backend
func (b *sshStorage) Name() string {
	return "SSH"
}

// Copy copies the given file to the SSH storage backend.
func (b *sshStorage) Copy(file string) (returnErr error) {
	source, err := os.Open(file)
	if err != nil {
		returnErr = errwrap.Wrap(err, "error reading the file to be uploaded")
		return
	}
	defer func() {
		returnErr = errors.Join(returnErr, source.Close())
	}()

	sourceFileInfo, err := source.Stat()
	if err != nil {
		returnErr = errwrap.Wrap(err, "error reading the source file stats")
		return
	}

	err = b.copyFile(source, sourceFileInfo.Size(), filepath.Base(file))
	for retry := 0; err != nil && retry < b.uploadRetries && retryableUploadError(err); retry++ {
		// The failed session (including its keepalive) must be stopped before
		// reconnecting. Its close error must not taint a successful later upload.
		_ = b.close()
		b.Log(storage.LogLevelWarning, b.Name(), "SSH upload failed, retrying (%d/%d) in %s: %v", retry+1, b.uploadRetries, b.uploadRetryDelay, err)
		time.Sleep(b.uploadRetryDelay)
		b.sftpClient, b.closeSession, err = b.connect()
		if err != nil {
			continue
		}
		err = b.copyFile(source, sourceFileInfo.Size(), filepath.Base(file))
	}
	if err != nil {
		return err
	}

	b.Log(storage.LogLevelInfo, b.Name(), "Uploaded a copy of backup `%s` to '%s' at path '%s'.", file, b.hostName, b.DestinationPath)
	return nil
}

func (b *sshStorage) copyFile(source *os.File, size int64, name string) (returnErr error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return errwrap.Wrap(err, "error rewinding the source file")
	}
	if err := b.sftpClient.MkdirAll(b.DestinationPath); err != nil {
		return errwrap.Wrap(err, "error ensuring destination directory")
	}
	// Create truncates a partial upload left by an earlier attempt.
	destination, err := b.sftpClient.Create(path.Join(b.DestinationPath, name))
	if err != nil {
		returnErr = errwrap.Wrap(err, "error creating file")
		return
	}
	defer func() {
		if err := destination.Close(); err != nil {
			returnErr = errors.Join(returnErr, errwrap.Wrap(err, "error closing the uploaded file"))
		}
	}()

	written, err := io.Copy(destination, source)
	if err != nil {
		returnErr = errwrap.Wrap(err, "error uploading the file")
		return
	}

	if written != size {
		msg := fmt.Sprintf(
			"failed to upload the file completely: wrote %d, expected %d",
			written,
			size,
		)

		returnErr = errwrap.Wrap(err, msg)
		return
	}

	return nil
}

// Retry only transport failures. A joined permanent error, such as a full disk
// followed by a failed close, must not become retryable because of the close.
func retryableUploadError(err error) bool {
	if err == nil {
		return false
	}
	switch err := err.(type) {
	case interface{ Unwrap() []error }:
		causes := err.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !retryableUploadError(cause) {
				return false
			}
		}
		return true
	case *os.PathError:
		return false // Local source open, read, stat, and seek errors.
	case *sftp.StatusError:
		return err.FxCode() == sftp.ErrSSHFxConnectionLost || err.FxCode() == sftp.ErrSSHFxNoConnection
	case net.Error:
		return true
	}
	switch err {
	case io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, net.ErrClosed,
		sftp.ErrSSHFxConnectionLost, sftp.ErrSSHFxNoConnection:
		return true
	}
	return retryableUploadError(errors.Unwrap(err))
}

// Prune rotates away backups according to the configuration and provided deadline for the SSH storage backend.
func (b *sshStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates, err := b.sftpClient.ReadDir(b.DestinationPath)
	if err != nil {
		// If directory doesn't exist yet, nothing to prune
		if errors.Is(err, os.ErrNotExist) {
			return &storage.PruneStats{}, nil
		}
		return nil, errwrap.Wrap(err, "error reading directory")
	}

	var matches []string
	var numCandidates int
	for _, candidate := range candidates {
		if candidate.IsDir() || !strings.HasPrefix(candidate.Name(), pruningPrefix) {
			continue
		}

		numCandidates++
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate.Name())
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(numCandidates),
		Pruned: uint(len(matches)),
	}

	pruneErr := b.DoPrune(b.Name(), len(matches), numCandidates, deadline, func() error {
		for _, match := range matches {
			p := path.Join(b.DestinationPath, match)
			if err := b.sftpClient.Remove(p); err != nil {
				return errwrap.Wrap(err, fmt.Sprintf("error removing file %s", p))
			}
		}
		return nil
	})

	return stats, pruneErr
}
