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
	s      *script
}

func newSshHelper(s *script) (*SshHelper, error) {
	if s.c.SSHHostName == "" {
		return nil, nil
	}

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
	r := &SshHelper{a, sshClient, s}
	a.Helper = r
	return r, nil
}

func (helper *SshHelper) copyArchive(name string) error {
	source, err := os.Open(helper.s.file)
	if err != nil {
		return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
	}
	defer source.Close()

	destination, err := helper.s.sftpClient.Create(filepath.Join(helper.s.c.SSHRemotePath, name))
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

	helper.s.logger.Infof("Uploaded a copy of backup `%s` to SSH storage '%s' at path '%s'.", helper.s.file, helper.s.c.SSHHostName, helper.s.c.SSHRemotePath)

	return nil
}

func (helper *SshHelper) pruneBackups(deadline time.Time) error {
	candidates, err := helper.s.sftpClient.ReadDir(helper.s.c.SSHRemotePath)
	if err != nil {
		return fmt.Errorf("pruneBackups: error reading directory from SSH storage: %w", err)
	}

	var matches []string
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), helper.s.c.BackupPruningPrefix) {
			continue
		}
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate.Name())
		}
	}

	helper.s.stats.Storages.SSH = StorageStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	doPrune(helper.s, len(matches), len(candidates), "SSH backup(s)", func() error {
		for _, match := range matches {
			if err := helper.s.sftpClient.Remove(filepath.Join(helper.s.c.SSHRemotePath, match)); err != nil {
				return fmt.Errorf("pruneBackups: error removing file from SSH storage: %w", err)
			}
		}
		return nil
	})

	return nil
}
