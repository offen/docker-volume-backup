package webdav

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	strg "github.com/offen/docker-volume-backup/internal/storage"
	t "github.com/offen/docker-volume-backup/internal/types"
	"github.com/sirupsen/logrus"
	"github.com/studio-b12/gowebdav"
)

type WebDavStorage struct {
	*strg.StorageBackend
	client *gowebdav.Client
}

// Specific init procedure for the WebDav storage provider.
func InitWebDav(c *t.Config, l *logrus.Logger, s *t.Stats) (*strg.StorageBackend, error) {
	if c.WebdavUsername == "" || c.WebdavPassword == "" {
		return nil, errors.New("newScript: WEBDAV_URL is defined, but no credentials were provided")
	} else {
		webdavClient := gowebdav.NewClient(c.WebdavUrl, c.WebdavUsername, c.WebdavPassword)

		if c.WebdavUrlInsecure {
			defaultTransport, ok := http.DefaultTransport.(*http.Transport)
			if !ok {
				return nil, errors.New("newScript: unexpected error when asserting type for http.DefaultTransport")
			}
			webdavTransport := defaultTransport.Clone()
			webdavTransport.TLSClientConfig.InsecureSkipVerify = c.WebdavUrlInsecure
			webdavClient.SetTransport(webdavTransport)
		}

		a := &strg.StorageBackend{
			Storage: &WebDavStorage{},
			Name:    "WebDav",
			Logger:  l,
			Config:  c,
			Stats:   s,
		}
		r := &WebDavStorage{a, webdavClient}
		a.Storage = r
		return a, nil
	}
}

// Specific copy function for the WebDav storage provider.
func (stg *WebDavStorage) Copy(file string) error {
	bytes, err := os.ReadFile(file)
	_, name := path.Split(file)
	if err != nil {
		return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
	}
	if err := stg.client.MkdirAll(stg.Config.WebdavPath, 0644); err != nil {
		return fmt.Errorf("copyBackup: error creating directory '%s' on WebDAV server: %w", stg.Config.WebdavPath, err)
	}
	if err := stg.client.Write(filepath.Join(stg.Config.WebdavPath, name), bytes, 0644); err != nil {
		return fmt.Errorf("copyBackup: error uploading the file to WebDAV server: %w", err)
	}
	stg.Logger.Infof("Uploaded a copy of backup `%s` to WebDAV-URL '%s' at path '%s'.", file, stg.Config.WebdavUrl, stg.Config.WebdavPath)

	return nil
}

// Specific prune function for the WebDav storage provider.
func (stg *WebDavStorage) Prune(deadline time.Time) error {
	candidates, err := stg.client.ReadDir(stg.Config.WebdavPath)
	if err != nil {
		return fmt.Errorf("pruneBackups: error looking up candidates from remote storage: %w", err)
	}
	var matches []fs.FileInfo
	var lenCandidates int
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), stg.Config.BackupPruningPrefix) {
			continue
		}
		lenCandidates++
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stg.Stats.Storages.WebDAV = t.StorageStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	stg.DoPrune(len(matches), lenCandidates, "WebDAV backup(s)", func() error {
		for _, match := range matches {
			if err := stg.client.Remove(filepath.Join(stg.Config.WebdavPath, match.Name())); err != nil {
				return fmt.Errorf("pruneBackups: error removing file from WebDAV storage: %w", err)
			}
		}
		return nil
	})

	return nil
}
