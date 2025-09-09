// Copyright 2025 - The Gemini CLI authors <gemini-cli@google.com>
// SPDX-License-Identifier: MPL-2.0

package googledrive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crypto/tls"
	"github.com/offen/docker-volume-backup/internal/errwrap"
	"github.com/offen/docker-volume-backup/internal/storage"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"net/http"
)

type googleDriveStorage struct {
	storage.StorageBackend
	client *drive.Service
}

// Config allows to configure a Google Drive storage backend.
type Config struct {
	CredentialsJSON    string
	FolderID           string
	ImpersonateSubject string
	Endpoint           string
	TokenURL           string
}

// NewStorageBackend creates and initializes a new Google Drive storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	ctx := context.Background()

	credentialsBytes := []byte(opts.CredentialsJSON)

	config, err := google.JWTConfigFromJSON(credentialsBytes, drive.DriveScope)
	if err != nil {
		return nil, errwrap.Wrap(err, "unable to parse credentials")
	}
	if opts.ImpersonateSubject != "" {
		config.Subject = opts.ImpersonateSubject
	}
	if opts.TokenURL != "" {
		config.TokenURL = opts.TokenURL
	}

	var clientOptions []option.ClientOption
	if opts.Endpoint != "" {
		clientOptions = append(clientOptions, option.WithEndpoint(opts.Endpoint))
		// Insecure transport for http mock server
		insecureTransport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		insecureClient := &http.Client{Transport: insecureTransport}
		ctx = context.WithValue(ctx, oauth2.HTTPClient, insecureClient)
	}
	clientOptions = append(clientOptions, option.WithTokenSource(config.TokenSource(ctx)))

	srv, err := drive.NewService(ctx, clientOptions...)
	if err != nil {
		return nil, errwrap.Wrap(err, "unable to create Drive client")
	}

	return &googleDriveStorage{
		StorageBackend: storage.StorageBackend{
			DestinationPath: opts.FolderID,
			Log:             logFunc,
		},
		client: srv,
	}, nil
}

// Name returns the name of the storage backend
func (b *googleDriveStorage) Name() string {
	return "GoogleDrive"
}

// Copy copies the given file to the Google Drive storage backend.
func (b *googleDriveStorage) Copy(file string) (returnErr error) {
	_, name := filepath.Split(file)
	b.Log(storage.LogLevelInfo, b.Name(), "Starting upload for backup '%s'.", name)

	f, err := os.Open(file)
	if err != nil {
		returnErr = errwrap.Wrap(err, fmt.Sprintf("failed to open file %s", file))
		return
	}
	defer func() {
		returnErr = f.Close()
	}()

	driveFile := &drive.File{Name: name}
	if b.DestinationPath != "" {
		driveFile.Parents = []string{b.DestinationPath}
	} else {
		driveFile.Parents = []string{"root"}
	}

	createCall := b.client.Files.Create(driveFile).SupportsAllDrives(true).Fields("id")
	created, err := createCall.Media(f).Do()
	if err != nil {
		returnErr = errwrap.Wrap(err, fmt.Sprintf("failed to upload %s", name))
		return
	}

	b.Log(storage.LogLevelInfo, b.Name(), "Finished upload for %s. File ID: %s", name, created.Id)
	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the Google Drive storage backend.
func (b *googleDriveStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	parentID := b.DestinationPath
	if parentID == "" {
		parentID = "root"
	}

	query := fmt.Sprintf("name contains '%s' and trashed = false", pruningPrefix)
	if parentID != "root" {
		query = fmt.Sprintf("'%s' in parents and (%s)", parentID, query)
	}

	var allFiles []*drive.File
	pageToken := ""
	for {
		req := b.client.Files.List().Q(query).SupportsAllDrives(true).Fields("files(id, name, createdTime, parents)").PageToken(pageToken)
		res, err := req.Do()
		if err != nil {
			return nil, errwrap.Wrap(err, "listing files")
		}
		allFiles = append(allFiles, res.Files...)
		pageToken = res.NextPageToken
		if pageToken == "" {
			break
		}
	}

	var matches []*drive.File
	var lenCandidates int
	for _, f := range allFiles {
		if !strings.HasPrefix(f.Name, pruningPrefix) {
			continue
		}
		lenCandidates++
		created, err := time.Parse(time.RFC3339, f.CreatedTime)
		if err != nil {
			b.Log(storage.LogLevelWarning, b.Name(), "Could not parse time for backup %s: %v", f.Name, err)
			continue
		}
		if created.Before(deadline) {
			matches = append(matches, f)
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	pruneErr := b.DoPrune(b.Name(), len(matches), lenCandidates, deadline, func() error {
		for _, file := range matches {
			b.Log(storage.LogLevelInfo, b.Name(), "Deleting old backup file: %s", file.Name)
			if err := b.client.Files.Delete(file.Id).SupportsAllDrives(true).Do(); err != nil {
				b.Log(storage.LogLevelWarning, b.Name(), "Error deleting %s: %v", file.Name, err)
			}
		}
		return nil
	})

	return stats, pruneErr
}
