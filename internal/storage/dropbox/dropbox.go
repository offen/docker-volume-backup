package dropbox

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"github.com/offen/docker-volume-backup/internal/storage"
)

type dropboxStorage struct {
	*storage.StorageBackend
	client files.Client
}

// Config allows to configure a Dropbox storage backend.
type Config struct {
	Token      string
	RemotePath string
}

// NewStorageBackend creates and initializes a new Dropbox storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	config := dropbox.Config{
		Token: opts.Token,
	}

	client := files.New(config)

	return &dropboxStorage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.RemotePath,
			Log:             logFunc,
		},
		client: client,
	}, nil
}

// Name returns the name of the storage backend
func (b *dropboxStorage) Name() string {
	return "Dropbox"
}

// Copy copies the given file to the WebDav storage backend.
func (b *dropboxStorage) Copy(file string) error {
	_, name := path.Split(file)

	folderArg := files.NewCreateFolderArg(b.DestinationPath)
	if _, err := b.client.CreateFolderV2(folderArg); err != nil {
		if err.(files.CreateFolderV2APIError).EndpointError.Path.Tag == files.WriteErrorConflict {
			b.Log(storage.LogLevelInfo, b.Name(), "Destination path '%s' already exists in Dropbox, no new directory required.", b.DestinationPath)
		} else {
			return fmt.Errorf("(*dropboxStorage).Copy: Error creating directory '%s' in Dropbox: %w", b.DestinationPath, err)
		}
	}

	r, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("(*dropboxStorage).Copy: Error opening the file to be uploaded: %w", err)
	}
	defer r.Close()

	// Start new upload session and get session id

	b.Log(storage.LogLevelInfo, b.Name(), "Starting upload session for backup '%s' to Dropbox at path '%s'.", file, b.DestinationPath)

	var sessionId string
	uploadSessionStartArg := files.NewUploadSessionStartArg()
	uploadSessionStartArg.SessionType = &files.UploadSessionType{Tagged: dropbox.Tagged{Tag: files.UploadSessionTypeConcurrent}}
	if res, err := b.client.UploadSessionStart(uploadSessionStartArg, nil); err != nil {
		return fmt.Errorf("(*dropboxStorage).Copy: Error starting the upload session: %w", err)
	} else {
		sessionId = res.SessionId
	}

	// Send the file in 148MB chunks (Dropbox API limit is 150MB, concurrent upload requires a multiple of 4MB though)
	// Last append can be any size <= 150MB with Close=True

	const chunkSize = 148 * 1024 * 1024 // 148MB
	var offset uint64 = 0

	for {
		chunk := make([]byte, chunkSize)
		bytesRead, err := r.Read(chunk)
		if err != nil {
			return fmt.Errorf("(*dropboxStorage).Copy: Error reading the file to be uploaded: %w", err)
		}
		chunk = chunk[:bytesRead]

		uploadSessionAppendArg := files.NewUploadSessionAppendArg(
			files.NewUploadSessionCursor(sessionId, offset),
		)
		isEOF := bytesRead < chunkSize
		uploadSessionAppendArg.Close = isEOF

		if err := b.client.UploadSessionAppendV2(uploadSessionAppendArg, bytes.NewReader(chunk)); err != nil {
			return fmt.Errorf("(*dropboxStorage).Copy: Error appending the file to the upload session: %w", err)
		}

		if isEOF {
			break
		}

		offset += uint64(bytesRead)
	}

	// Finish the upload session, commit the file (no new data added)

	b.client.UploadSessionFinish(
		files.NewUploadSessionFinishArg(
			files.NewUploadSessionCursor(sessionId, 0),
			files.NewCommitInfo(filepath.Join(b.DestinationPath, name)),
		), nil)

	b.Log(storage.LogLevelInfo, b.Name(), "Uploaded a copy of backup '%s' to Dropbox at path '%s'.", file, b.DestinationPath)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the Dropbox storage backend.
func (b *dropboxStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	var entries []files.IsMetadata
	res, err := b.client.ListFolder(files.NewListFolderArg(b.DestinationPath))
	if err != nil {
		return nil, fmt.Errorf("(*webDavStorage).Prune: Error looking up candidates from remote storage: %w", err)
	}
	entries = append(entries, res.Entries...)

	for res.HasMore {
		res, err = b.client.ListFolderContinue(files.NewListFolderContinueArg(res.Cursor))
		if err != nil {
			return nil, fmt.Errorf("(*webDavStorage).Prune: Error looking up candidates from remote storage: %w", err)
		}
		entries = append(entries, res.Entries...)
	}

	var matches []*files.FileMetadata
	var lenCandidates int
	for _, candidate := range entries {
		if reflect.Indirect(reflect.ValueOf(candidate)).Type() != reflect.TypeOf(files.FileMetadata{}) {
			continue
		}
		candidate := candidate.(*files.FileMetadata)
		if !strings.HasPrefix(candidate.Name, pruningPrefix) {
			continue
		}
		lenCandidates++
		if candidate.ServerModified.Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	if err := b.DoPrune(b.Name(), len(matches), lenCandidates, "Dropbox backup(s)", func() error {
		for _, match := range matches {
			if _, err := b.client.DeleteV2(files.NewDeleteArg(filepath.Join(b.DestinationPath, match.Name))); err != nil {
				return fmt.Errorf("(*dropboxStorage).Prune: Error removing file from Dropbox storage: %w", err)
			}
		}
		return nil
	}); err != nil {
		return stats, err
	}

	return stats, nil
}
