package ssh

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	strg "github.com/offen/docker-volume-backup/internal/storage"
	t "github.com/offen/docker-volume-backup/internal/types"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type SSHStorage struct {
	*strg.StorageBackend
	client     *ssh.Client
	sftpClient *sftp.Client
}

// Specific init procedure for the SSH storage provider.
func InitSSH(c *t.Config, l *logrus.Logger, s *t.Stats) (*strg.StorageBackend, error) {
	var authMethods []ssh.AuthMethod

	if c.SSHPassword != "" {
		authMethods = append(authMethods, ssh.Password(c.SSHPassword))
	}

	if _, err := os.Stat(c.SSHIdentityFile); err == nil {
		key, err := ioutil.ReadFile(c.SSHIdentityFile)
		if err != nil {
			return nil, errors.New("newScript: error reading the private key")
		}

		var signer ssh.Signer
		if c.SSHIdentityPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(c.SSHIdentityPassphrase))
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
		User:            c.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", c.SSHHostName, c.SSHPort), sshClientConfig)

	if err != nil {
		return nil, fmt.Errorf("newScript: error creating ssh client: %w", err)
	}
	_, _, err = sshClient.SendRequest("keepalive", false, nil)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("newScript: error creating sftp client: %w", err)
	}

	a := &strg.StorageBackend{
		Storage: &SSHStorage{},
		Name:    "SSH",
		Logger:  l,
		Config:  c,
		Stats:   s,
	}
	r := &SSHStorage{a, sshClient, sftpClient}
	a.Storage = r
	return a, nil
}

// Specific copy function for the SSH storage provider.
func (stg *SSHStorage) Copy(file string) error {
	source, err := os.Open(file)
	_, name := path.Split(file)
	if err != nil {
		return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
	}
	defer source.Close()

	destination, err := stg.sftpClient.Create(filepath.Join(stg.Config.SSHRemotePath, name))
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

	stg.Logger.Infof("Uploaded a copy of backup `%s` to SSH storage '%s' at path '%s'.", file, stg.Config.SSHHostName, stg.Config.SSHRemotePath)

	return nil
}

// Specific prune function for the SSH storage provider.
func (stg *SSHStorage) Prune(deadline time.Time) error {
	candidates, err := stg.sftpClient.ReadDir(stg.Config.SSHRemotePath)
	if err != nil {
		return fmt.Errorf("pruneBackups: error reading directory from SSH storage: %w", err)
	}

	var matches []string
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), stg.Config.BackupPruningPrefix) {
			continue
		}
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate.Name())
		}
	}

	stg.Stats.Storages.SSH = t.StorageStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	stg.DoPrune(len(matches), len(candidates), "SSH backup(s)", func() error {
		for _, match := range matches {
			if err := stg.sftpClient.Remove(filepath.Join(stg.Config.SSHRemotePath, match)); err != nil {
				return fmt.Errorf("pruneBackups: error removing file from SSH storage: %w", err)
			}
		}
		return nil
	})

	return nil
}
