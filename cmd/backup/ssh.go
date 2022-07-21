package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type SshHelper struct {
	*AbstractHelper
	client *ssh.Client
}

func newSshHelper(client *ssh.Client) *SshHelper {
	a := &AbstractHelper{}
	r := &SshHelper{a, client}
	a.Helper = r
	return r
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
