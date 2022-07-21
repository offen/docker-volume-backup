package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SshHelper struct {
	*AbstractHelper
	client *ssh.Client
}

func newSshHelper(s *script) (*SshHelper, error) {
	var authMethods []ssh.AuthMethod

	if s.c.SSHPassword != "" {
		authMethods = append(authMethods, ssh.Password(s.c.SSHPassword))
	}

	if _, err := os.Stat(s.c.SSHIdentityFile); err == nil {
		key, err := ioutil.ReadFile(s.c.SSHIdentityFile)
		if err != nil {
			return nil, errors.New("newScript: error reading the private key")
		}

		var signer ssh.Signer
		if s.c.SSHIdentityPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(s.c.SSHIdentityPassphrase))
			if err != nil {
				return nil, errors.New("newScript: error parsing the encrypted private key")
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
			if err != nil {
				return nil, errors.New("newScript: error parsing the private key")
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	sshClientConfig := &ssh.ClientConfig{
		User:            s.c.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", s.c.SSHHostName, s.c.SSHPort), sshClientConfig)

	if err != nil {
		return nil, fmt.Errorf("newScript: error creating ssh client: %w", err)
	}
	_, _, err = sshClient.SendRequest("keepalive", false, nil)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient)
	s.sftpClient = sftpClient
	if err != nil {
		return nil, fmt.Errorf("newScript: error creating sftp client: %w", err)
	}

	a := &AbstractHelper{}
	r := &SshHelper{a, sshClient}
	a.Helper = r
	return r, nil
}

func (helper *SshHelper) copyArchive(s *script, name string) error {
	source, err := os.Open(s.file)
	if err != nil {
		return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
	}
	defer source.Close()

	destination, err := s.sftpClient.Create(filepath.Join(s.c.SSHRemotePath, name))
	if err != nil {
		return fmt.Errorf("copyBackup: error creating file on SSH storage: %w", err)
	}
	defer destination.Close()

	chunk := make([]byte, 1000000)
	for {
		num, err := source.Read(chunk)
		if err == io.EOF {
			tot, err := destination.Write(chunk[:num])
			if err != nil {
				return fmt.Errorf("copyBackup: error uploading the file to SSH storage: %w", err)
			}

			if tot != len(chunk[:num]) {
				return fmt.Errorf("sshClient: failed to write stream")
			}

			break
		}

		if err != nil {
			return fmt.Errorf("copyBackup: error uploading the file to SSH storage: %w", err)
		}

		tot, err := destination.Write(chunk[:num])
		if err != nil {
			return fmt.Errorf("copyBackup: error uploading the file to SSH storage: %w", err)
		}

		if tot != len(chunk[:num]) {
			return fmt.Errorf("sshClient: failed to write stream")
		}
	}

	s.logger.Infof("Uploaded a copy of backup `%s` to SSH storage '%s' at path '%s'.", s.file, s.c.SSHHostName, s.c.SSHRemotePath)

	return nil
}

func (helper *SshHelper) pruneBackups(s *script, deadline time.Time) error {
	candidates, err := s.sftpClient.ReadDir(s.c.SSHRemotePath)
	if err != nil {
		return fmt.Errorf("pruneBackups: error reading directory from SSH storage: %w", err)
	}

	var matches []string
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), s.c.BackupPruningPrefix) {
			continue
		}
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate.Name())
		}
	}

	s.stats.Storages.SSH = StorageStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	doPrune(s, len(matches), len(candidates), "SSH backup(s)", func() error {
		for _, match := range matches {
			if err := s.sftpClient.Remove(filepath.Join(s.c.SSHRemotePath, match)); err != nil {
				return fmt.Errorf("pruneBackups: error removing file from SSH storage: %w", err)
			}
		}
		return nil
	})

	return nil
}
