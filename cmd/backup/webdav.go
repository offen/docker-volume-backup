package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/studio-b12/gowebdav"
)

type WebdavHelper struct {
	*AbstractHelper
	client *gowebdav.Client
}

func newWebdavHelper(client *gowebdav.Client) *WebdavHelper {
	a := &AbstractHelper{}
	r := &WebdavHelper{a, client}
	a.Helper = r
	return r
}

func (helper *WebdavHelper) copyArchive(s *script, name string) error {
	bytes, err := os.ReadFile(s.file)
	if err != nil {
		return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
	}
	if err := helper.client.MkdirAll(s.c.WebdavPath, 0644); err != nil {
		return fmt.Errorf("copyBackup: error creating directory '%s' on WebDAV server: %w", s.c.WebdavPath, err)
	}
	if err := helper.client.Write(filepath.Join(s.c.WebdavPath, name), bytes, 0644); err != nil {
		return fmt.Errorf("copyBackup: error uploading the file to WebDAV server: %w", err)
	}
	s.logger.Infof("Uploaded a copy of backup `%s` to WebDAV-URL '%s' at path '%s'.", s.file, s.c.WebdavUrl, s.c.WebdavPath)

	return nil
}

func (helper *WebdavHelper) pruneBackups(s *script, deadline time.Time) error {
	candidates, err := helper.client.ReadDir(s.c.WebdavPath)
	if err != nil {
		return fmt.Errorf("pruneBackups: error looking up candidates from remote storage: %w", err)
	}
	var matches []fs.FileInfo
	var lenCandidates int
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), s.c.BackupPruningPrefix) {
			continue
		}
		lenCandidates++
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	s.stats.Storages.WebDAV = StorageStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	doPrune(s, len(matches), lenCandidates, "WebDAV backup(s)", func() error {
		for _, match := range matches {
			if err := helper.client.Remove(filepath.Join(s.c.WebdavPath, match.Name())); err != nil {
				return fmt.Errorf("pruneBackups: error removing file from WebDAV storage: %w", err)
			}
		}
		return nil
	})

	return nil
}
