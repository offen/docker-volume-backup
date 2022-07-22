package storages

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

	t "github.com/offen/docker-volume-backup/cmd/backup/types"
	"github.com/studio-b12/gowebdav"
)

type WebDavStorage struct {
	*GenericStorage
	client     *gowebdav.Client
	webdavUrl  string
	webdavPath string
}

func InitWebDav(c *t.Config) (*WebDavStorage, error) {
	if c.WebdavUrl == "" {
		return nil, nil
	}

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

		a := &GenericStorage{}
		r := &WebDavStorage{a, webdavClient, c.WebdavUrl, c.WebdavPath}
		a.Storage = r
		return r, nil
	}
}

func (wd *WebDavStorage) Copy(file string) error {
	bytes, err := os.ReadFile(file)
	_, name := path.Split(file)
	if err != nil {
		return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
	}
	if err := wd.client.MkdirAll(wd.webdavPath, 0644); err != nil {
		return fmt.Errorf("copyBackup: error creating directory '%s' on WebDAV server: %w", wd.webdavPath, err)
	}
	if err := wd.client.Write(filepath.Join(wd.webdavPath, name), bytes, 0644); err != nil {
		return fmt.Errorf("copyBackup: error uploading the file to WebDAV server: %w", err)
	}
	wd.logger.Infof("Uploaded a copy of backup `%s` to WebDAV-URL '%s' at path '%s'.", file, wd.webdavUrl, wd.webdavPath)

	return nil
}

func (wd *WebDavStorage) Prune(deadline time.Time) (*t.StorageStats, error) {
	candidates, err := wd.client.ReadDir(wd.webdavPath)
	if err != nil {
		return nil, fmt.Errorf("pruneBackups: error looking up candidates from remote storage: %w", err)
	}
	var matches []fs.FileInfo
	var lenCandidates int
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), wd.backupPruningPrefix) {
			continue
		}
		lenCandidates++
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stats := t.StorageStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	wd.doPrune(len(matches), lenCandidates, "WebDAV backup(s)", func() error {
		for _, match := range matches {
			if err := wd.client.Remove(filepath.Join(wd.webdavPath, match.Name())); err != nil {
				return fmt.Errorf("pruneBackups: error removing file from WebDAV storage: %w", err)
			}
		}
		return nil
	})

	return &stats, nil
}
