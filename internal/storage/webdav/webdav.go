// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

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

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/studio-b12/gowebdav"
)

type webDavStorage struct {
	*storage.StorageBackend
	client *gowebdav.Client
	url    string
}

// Config allows to configure a WebDAV storage backend.
type Config struct {
	URL         string
	RemotePath  string
	Username    string
	Password    string
	URLInsecure bool
}

// NewStorageBackend creates and initializes a new WebDav storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {

	if opts.Username == "" || opts.Password == "" {
		return nil, errors.New("NewStorageBackend: WEBDAV_URL is defined, but no credentials were provided")
	} else {
		webdavClient := gowebdav.NewClient(opts.URL, opts.Username, opts.Password)

		if opts.URLInsecure {
			defaultTransport, ok := http.DefaultTransport.(*http.Transport)
			if !ok {
				return nil, errors.New("NewStorageBackend: unexpected error when asserting type for http.DefaultTransport")
			}
			webdavTransport := defaultTransport.Clone()
			webdavTransport.TLSClientConfig.InsecureSkipVerify = opts.URLInsecure
			webdavClient.SetTransport(webdavTransport)
		}

		return &webDavStorage{
			StorageBackend: &storage.StorageBackend{
				DestinationPath: opts.RemotePath,
				Log:             logFunc,
			},
			client: webdavClient,
		}, nil
	}
}

// Name returns the name of the storage backend
func (b *webDavStorage) Name() string {
	return "WebDAV"
}

// Copy copies the given file to the WebDav storage backend.
func (b *webDavStorage) Copy(file string) error {
	bytes, err := os.ReadFile(file)
	_, name := path.Split(file)
	if err != nil {
		return fmt.Errorf("(*webDavStorage).Copy: Error reading the file to be uploaded! %w", err)
	}
	if err := b.client.MkdirAll(b.DestinationPath, 0644); err != nil {
		return fmt.Errorf("(*webDavStorage).Copy: Error creating directory '%s' on WebDAV server! %w", b.DestinationPath, err)
	}
	if err := b.client.Write(filepath.Join(b.DestinationPath, name), bytes, 0644); err != nil {
		return fmt.Errorf("(*webDavStorage).Copy: Error uploading the file to WebDAV server! %w", err)
	}
	b.Log(storage.LogLevelInfo, b.Name(), "Uploaded a copy of backup `%s` to WebDAV-URL '%s' at path '%s'.", file, b.url, b.DestinationPath)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the WebDav storage backend.
func (b *webDavStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates, err := b.client.ReadDir(b.DestinationPath)
	if err != nil {
		return nil, fmt.Errorf("(*webDavStorage).Prune: Error looking up candidates from remote storage! %w", err)
	}
	var matches []fs.FileInfo
	var lenCandidates int
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Name(), pruningPrefix) {
			continue
		}
		lenCandidates++
		if candidate.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	if err := b.DoPrune(b.Name(), len(matches), lenCandidates, "WebDAV backup(s)", func() error {
		for _, match := range matches {
			if err := b.client.Remove(filepath.Join(b.DestinationPath, match.Name())); err != nil {
				return fmt.Errorf("(*webDavStorage).Prune: Error removing file from WebDAV storage! %w", err)
			}
		}
		return nil
	}); err != nil {
		return stats, err
	}

	return stats, nil
}
