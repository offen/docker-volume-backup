// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
	"github.com/jattento/docker-volume-backup/internal/storage"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sshStorage struct {
	*storage.StorageBackend
	client     *ssh.Client
	sftpClient *sftp.Client
	hostName   string
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
}

// NewStorageBackend creates and initializes a new SSH storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	var authMethods []ssh.AuthMethod

	if opts.Password != "" {
		authMethods = append(authMethods, ssh.Password(opts.Password))
	}

	if _, err := os.Stat(opts.IdentityFile); err == nil {
		key, err := os.ReadFile(opts.IdentityFile)
		if err != nil {
			return nil, errwrap.Wrap(nil, "error reading the private key")
		}

		var signer ssh.Signer
		if opts.IdentityPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(opts.IdentityPassphrase))
			if err != nil {
				return nil, errwrap.Wrap(nil, "error parsing the encrypted private key")
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
			if err != nil {
				return nil, errwrap.Wrap(nil, "error parsing the private key")
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	sshClientConfig := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", opts.HostName, opts.Port), sshClientConfig)

	if err != nil {
		return nil, errwrap.Wrap(err, "error creating ssh client")
	}
	_, _, err = sshClient.SendRequest("keepalive", false, nil)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	)
	if err != nil {
		return nil, errwrap.Wrap(err, "error creating sftp client")
	}

	return &sshStorage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.RemotePath,
			Log:             logFunc,
		},
		client:     sshClient,
		sftpClient: sftpClient,
		hostName:   opts.HostName,
	}, nil
}

// Name returns the name of the storage backend
func (b *sshStorage) Name() string {
	return "SSH"
}

// Copy copies the given file to the SSH storage backend.
func (b *sshStorage) Copy(file string) error {
	source, err := os.Open(file)
	_, name := path.Split(file)
	if err != nil {
		return errwrap.Wrap(err, " error reading the file to be uploaded")
	}
	defer source.Close()

	destination, err := b.sftpClient.Create(filepath.Join(b.DestinationPath, name))
	if err != nil {
		return errwrap.Wrap(err, "error creating file")
	}
	defer destination.Close()

	chunk := make([]byte, 1e9)
	for {
		num, err := source.Read(chunk)
		if err == io.EOF {
			tot, err := destination.Write(chunk[:num])
			if err != nil {
				return errwrap.Wrap(err, "error uploading the file")
			}

			if tot != len(chunk[:num]) {
				return errwrap.Wrap(nil, "failed to write stream")
			}

			break
		}

		if err != nil {
			return errwrap.Wrap(err, "error uploading the file")
		}

		tot, err := destination.Write(chunk[:num])
		if err != nil {
			return errwrap.Wrap(err, "error uploading the file")
		}

		if tot != len(chunk[:num]) {
			return errwrap.Wrap(nil, "failed to write stream")
		}
	}

	b.Log(storage.LogLevelInfo, b.Name(), "Uploaded a copy of backup `%s` to '%s' at path '%s'.", file, b.hostName, b.DestinationPath)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the SSH storage backend.
func (b *sshStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates, err := b.sftpClient.ReadDir(b.DestinationPath)
	if err != nil {
		return nil, errwrap.Wrap(err, "error reading directory")
	}

	var matches []string
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), pruningPrefix) {
			continue
		}
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate.Name())
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	pruneErr := b.DoPrune(b.Name(), len(matches), len(candidates), deadline, func() error {
		for _, match := range matches {
			if err := b.sftpClient.Remove(filepath.Join(b.DestinationPath, match)); err != nil {
				return errwrap.Wrap(err, "error removing file")
			}
		}
		return nil
	})

	return stats, pruneErr
}
