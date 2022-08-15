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
func (stg *webDavStorage) Name() string {
	return "WebDav"
}

// Copy copies the given file to the WebDav storage backend.
func (stg *webDavStorage) Copy(file string) error {
	bytes, err := os.ReadFile(file)
	_, name := path.Split(file)
	if err != nil {
		return stg.Log(storage.ERROR, stg.Name(), "Copy: Error reading the file to be uploaded! %w", err)
	}
	if err := stg.client.MkdirAll(stg.DestinationPath, 0644); err != nil {
		return stg.Log(storage.ERROR, stg.Name(), "Copy: Error creating directory '%s' on WebDAV server! %w", stg.DestinationPath, err)
	}
	if err := stg.client.Write(filepath.Join(stg.DestinationPath, name), bytes, 0644); err != nil {
		return stg.Log(storage.ERROR, stg.Name(), "Copy: Error uploading the file to WebDAV server! %w", err)
	}
	stg.Log(storage.INFO, stg.Name(), "Uploaded a copy of backup `%s` to WebDAV-URL '%s' at path '%s'.", file, stg.url, stg.DestinationPath)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the WebDav storage backend.
func (stg *webDavStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates, err := stg.client.ReadDir(stg.DestinationPath)
	if err != nil {
		return nil, stg.Log(storage.ERROR, stg.Name(), "Prune: Error looking up candidates from remote storage! %w", err)
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

	stg.DoPrune(len(matches), lenCandidates, "WebDAV backup(s)", func() error {
		for _, match := range matches {
			if err := stg.client.Remove(filepath.Join(stg.DestinationPath, match.Name())); err != nil {
				return stg.Log(storage.ERROR, stg.Name(), "Prune: Error removing file from WebDAV storage! %w", err)
			}
		}
		return nil
	})

	return stats, nil
}
