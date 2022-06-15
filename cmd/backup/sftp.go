package main

import (
	"fmt"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"io"
	"os"
)

func (s *script) sshConnect() error {
	if s.sshClient != nil {
		_, _, err := s.sshClient.SendRequest("keepalive", false, nil)
		if err == nil {
			return nil
		}
	}

	cfg := &ssh.ClientConfig{
		User:            s.c.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.Password(s.c.SSHPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%v:%v", s.c.SSHHostName, s.c.SSHPort), cfg)
	if err != nil {
		return fmt.Errorf("sshClient: ssh dial: %w", err)
	}
	s.sshClient = sshClient

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("sshClient: sftp new client: %w", err)
	}
	s.sftpClient = sftpClient

	return nil
}

func (s *script) sshUpload(source io.Reader, destination io.Writer, size int) error {
	if err := s.sshConnect(); err != nil {
		return err
	}

	chunk := make([]byte, size)

	for {
		num, err := source.Read(chunk)
		if err == io.EOF {
			tot, err := destination.Write(chunk[:num])
			if err != nil {
				return err
			}

			if tot != len(chunk[:num]) {
				return fmt.Errorf("sshClient: failed to write stream")
			}

			return nil
		}

		if err != nil {
			return err
		}

		tot, err := destination.Write(chunk[:num])
		if err != nil {
			return err
		}

		if tot != len(chunk[:num]) {
			return fmt.Errorf("sshClient: failed to write stream")
		}
	}
}

func (s *script) sshCreateFile(filePath string) (io.ReadWriteCloser, error) {
	if err := s.sshConnect(); err != nil {
		return nil, err
	}

	return s.sftpClient.Create(filePath)
}

func (s *script) sshDeleteFile(filePath string) error {
	if err := s.sshConnect(); err != nil {
		return err
	}

	return s.sftpClient.Remove(filePath)
}

func (s *script) sshReadDir(filePath string) ([]os.FileInfo, error) {
	if err := s.sshConnect(); err != nil {
		return nil, err
	}

	dir, err := s.sftpClient.ReadDir(filePath)
	if err != nil {
		return nil, fmt.Errorf("sshClient: dir stats: %w", err)
	}

	return dir, nil
}
