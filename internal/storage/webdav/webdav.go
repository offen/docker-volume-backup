package webdav

import (
	"errors"
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

// NewStorageBackend creates and initializes a new WebDav storage backend.
func NewStorageBackend(url string, remotePath string, username string, password string, urlInsecure bool,
	logFunc storage.LogFuncDef) (storage.Backend, error) {

	if username == "" || password == "" {
		return nil, errors.New("newScript: WEBDAV_URL is defined, but no credentials were provided")
	} else {
		webdavClient := gowebdav.NewClient(url, username, password)

		if urlInsecure {
			defaultTransport, ok := http.DefaultTransport.(*http.Transport)
			if !ok {
				return nil, errors.New("newScript: unexpected error when asserting type for http.DefaultTransport")
			}
			webdavTransport := defaultTransport.Clone()
			webdavTransport.TLSClientConfig.InsecureSkipVerify = urlInsecure
			webdavClient.SetTransport(webdavTransport)
		}

		strgBackend := &storage.StorageBackend{
			Backend:         &webDavStorage{},
			DestinationPath: remotePath,
			Log:             logFunc,
		}
		webdavBackend := &webDavStorage{
			StorageBackend: strgBackend,
			client:         webdavClient,
		}
		strgBackend.Backend = webdavBackend
		return strgBackend, nil
	}
}

// Name returns the name of the storage backend
func (b *webDavStorage) Name() string {
	return "WebDav"
}

// Copy copies the given file to the WebDav storage backend.
func (b *webDavStorage) Copy(file string) error {
	bytes, err := os.ReadFile(file)
	_, name := path.Split(file)
	if err != nil {
		return b.Log(storage.ERROR, b.Name(), "Copy: Error reading the file to be uploaded! %w", err)
	}
	if err := b.client.MkdirAll(b.DestinationPath, 0644); err != nil {
		return b.Log(storage.ERROR, b.Name(), "Copy: Error creating directory '%s' on WebDAV server! %w", b.DestinationPath, err)
	}
	if err := b.client.Write(filepath.Join(b.DestinationPath, name), bytes, 0644); err != nil {
		return b.Log(storage.ERROR, b.Name(), "Copy: Error uploading the file to WebDAV server! %w", err)
	}
	b.Log(storage.INFO, b.Name(), "Uploaded a copy of backup `%s` to WebDAV-URL '%s' at path '%s'.", file, b.url, b.DestinationPath)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the WebDav storage backend.
func (b *webDavStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates, err := b.client.ReadDir(b.DestinationPath)
	if err != nil {
		return nil, b.Log(storage.ERROR, b.Name(), "Prune: Error looking up candidates from remote storage! %w", err)
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

	b.DoPrune(len(matches), lenCandidates, "WebDAV backup(s)", func() error {
		for _, match := range matches {
			if err := b.client.Remove(filepath.Join(b.DestinationPath, match.Name())); err != nil {
				return b.Log(storage.ERROR, b.Name(), "Prune: Error removing file from WebDAV storage! %w", err)
			}
		}
		return nil
	})

	return stats, nil
}
