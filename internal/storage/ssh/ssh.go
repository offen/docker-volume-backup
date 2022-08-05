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

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sshStorage struct {
	*storage.StorageBackend
	client     *ssh.Client
	sftpClient *sftp.Client
	hostName   string
}

// NewStorageBackend creates and initializes a new SSH storage backend.
func NewStorageBackend(hostName string, port string, user string, password string, identityFile string, identityPassphrase string, remotePath string,
	logFunc storage.LogFuncDef) (storage.Backend, error) {

	var authMethods []ssh.AuthMethod

	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	if _, err := os.Stat(identityFile); err == nil {
		key, err := ioutil.ReadFile(identityFile)
		if err != nil {
			return nil, errors.New("newScript: error reading the private key")
		}

		var signer ssh.Signer
		if identityPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(identityPassphrase))
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
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", hostName, port), sshClientConfig)

	if err != nil {
		return nil, logFunc(storage.ERROR, "SSH", "NewScript: Error creating ssh client! %w", err)
	}
	_, _, err = sshClient.SendRequest("keepalive", false, nil)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, logFunc(storage.ERROR, "SSH", "NewScript: error creating sftp client! %w", err)
	}

	strgBackend := &storage.StorageBackend{
		Backend:         &sshStorage{},
		Name:            "SSH",
		DestinationPath: remotePath,
		Log:             logFunc,
	}
	sshBackend := &sshStorage{
		StorageBackend: strgBackend,
		client:         sshClient,
		sftpClient:     sftpClient,
		hostName:       hostName,
	}
	strgBackend.Backend = sshBackend
	return strgBackend, nil
}

func (stg *sshStorage) GetName() string {
	return stg.Name
}

// Copy copies the given file to the SSH storage backend.
func (stg *sshStorage) Copy(file string) error {
	source, err := os.Open(file)
	_, name := path.Split(file)
	if err != nil {
		return stg.Log(storage.ERROR, stg.Name, "Copy: Error reading the file to be uploaded! %w", err)
	}
	defer source.Close()

	destination, err := stg.sftpClient.Create(filepath.Join(stg.DestinationPath, name))
	if err != nil {
		return stg.Log(storage.ERROR, stg.Name, "Copy: Error creating file on SSH storage! %w", err)
	}
	defer destination.Close()

	chunk := make([]byte, 1000000)
	for {
		num, err := source.Read(chunk)
		if err == io.EOF {
			tot, err := destination.Write(chunk[:num])
			if err != nil {
				return stg.Log(storage.ERROR, stg.Name, "Copy: Error uploading the file to SSH storage! %w", err)
			}

			if tot != len(chunk[:num]) {
				return stg.Log(storage.ERROR, stg.Name, "sshClient: failed to write stream")
			}

			break
		}

		if err != nil {
			return stg.Log(storage.ERROR, stg.Name, "Copy: Error uploading the file to SSH storage! %w", err)
		}

		tot, err := destination.Write(chunk[:num])
		if err != nil {
			return stg.Log(storage.ERROR, stg.Name, "Copy: Error uploading the file to SSH storage! %w", err)
		}

		if tot != len(chunk[:num]) {
			return stg.Log(storage.ERROR, stg.Name, "sshClient: failed to write stream")
		}
	}

	stg.Log(storage.INFO, stg.Name, "Uploaded a copy of backup `%s` to SSH storage '%s' at path '%s'.", file, stg.hostName, stg.DestinationPath)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the SSH storage backend.
func (stg *sshStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates, err := stg.sftpClient.ReadDir(stg.DestinationPath)
	if err != nil {
		return nil, stg.Log(storage.ERROR, stg.Name, "Prune: Error reading directory from SSH storage! %w", err)
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

	stg.DoPrune(len(matches), len(candidates), "SSH backup(s)", func() error {
		for _, match := range matches {
			if err := stg.sftpClient.Remove(filepath.Join(stg.DestinationPath, match)); err != nil {
				return stg.Log(storage.ERROR, stg.Name, "Prune: Error removing file from SSH storage! %w", err)
			}
		}
		return nil
	})

	return stats, nil
}
