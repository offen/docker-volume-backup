package dropbox

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"github.com/offen/docker-volume-backup/internal/storage"
	"golang.org/x/oauth2"
)

type dropboxStorage struct {
	*storage.StorageBackend
	client           files.Client
	concurrencyLevel int
}

// Config allows to configure a Dropbox storage backend.
type Config struct {
	Endpoint         string
	RefreshToken     string
	AppKey           string
	AppSecret        string
	RemotePath       string
	ConcurrencyLevel int
}

// NewStorageBackend creates and initializes a new Dropbox storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	tokenUrl, _ := url.JoinPath(opts.Endpoint, "oauth2/token")

	conf := &oauth2.Config{
		ClientID:     opts.AppKey,
		ClientSecret: opts.AppSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenUrl,
		},
	}

	isCITest := opts.Endpoint != "https://api.dropbox.com/"

	logFunc(storage.LogLevelInfo, "Dropbox", "Fetching fresh access token for Dropbox storage backend.")
	token := &oauth2.Token{RefreshToken: opts.RefreshToken}
	if !isCITest {
		tkSource := conf.TokenSource(context.Background(), &oauth2.Token{RefreshToken: opts.RefreshToken})
		var err error
		token, err = tkSource.Token()
		if err != nil {
			return nil, fmt.Errorf("(*dropboxStorage).NewStorageBackend: Error refreshing token: %w", err)
		}
	}

	dbxConfig := dropbox.Config{}

	if isCITest {
		dbxConfig.Token = opts.RefreshToken
		dbxConfig.URLGenerator = func(hostType string, namespace string, route string) string {
			return fmt.Sprintf("%s/%d/%s/%s", opts.Endpoint, 2, namespace, route)
		}
	} else {
		dbxConfig.Token = token.AccessToken
	}

	client := files.New(dbxConfig)

	if opts.ConcurrencyLevel < 1 {
		logFunc(storage.LogLevelWarning, "Dropbox", "Concurrency level must be at least 1! Using 1 instead of %d.", opts.ConcurrencyLevel)
		opts.ConcurrencyLevel = 1
	}

	return &dropboxStorage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.RemotePath,
			Log:             logFunc,
		},
		client:           client,
		concurrencyLevel: opts.ConcurrencyLevel,
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
		switch err := err.(type) {
		case files.CreateFolderV2APIError:
			if err.EndpointError.Path.Tag != files.WriteErrorConflict {
				return fmt.Errorf("(*dropboxStorage).Copy: Error creating directory '%s' in Dropbox: %w", b.DestinationPath, err)
			}
			b.Log(storage.LogLevelInfo, b.Name(), "Destination path '%s' already exists in Dropbox, no new directory required.", b.DestinationPath)
		default:
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
	var guard = make(chan struct{}, b.concurrencyLevel)
	var errorChn = make(chan error, b.concurrencyLevel)
	var EOFChn = make(chan bool, b.concurrencyLevel)
	var mu sync.Mutex
	var wg sync.WaitGroup

loop:
	for {
		guard <- struct{}{} // limit concurrency
		select {
		case err := <-errorChn: // error from goroutine
			return err
		case <-EOFChn: // EOF from goroutine
			wg.Wait() // wait for all goroutines to finish
			break loop
		default:
		}

		go func() {
			defer func() {
				wg.Done()
				<-guard
			}()
			wg.Add(1)
			chunk := make([]byte, chunkSize)

			mu.Lock() // to preserve offset of chunks

			select {
			case <-EOFChn:
				EOFChn <- true // put it back for outer loop
				mu.Unlock()
				return // already EOF
			default:
			}

			bytesRead, err := r.Read(chunk)
			if err != nil {
				errorChn <- fmt.Errorf("(*dropboxStorage).Copy: Error reading the file to be uploaded: %w", err)
				mu.Unlock()
				return
			}
			chunk = chunk[:bytesRead]

			uploadSessionAppendArg := files.NewUploadSessionAppendArg(
				files.NewUploadSessionCursor(sessionId, offset),
			)
			isEOF := bytesRead < chunkSize
			uploadSessionAppendArg.Close = isEOF
			if isEOF {
				EOFChn <- true
			}
			offset += uint64(bytesRead)

			mu.Unlock()

			if err := b.client.UploadSessionAppendV2(uploadSessionAppendArg, bytes.NewReader(chunk)); err != nil {
				errorChn <- fmt.Errorf("(*dropboxStorage).Copy: Error appending the file to the upload session: %w", err)
				return
			}
		}()
	}

	// Finish the upload session, commit the file (no new data added)

	_, err = b.client.UploadSessionFinish(
		files.NewUploadSessionFinishArg(
			files.NewUploadSessionCursor(sessionId, 0),
			files.NewCommitInfo(filepath.Join(b.DestinationPath, name)),
		), nil)
	if err != nil {
		return fmt.Errorf("(*dropboxStorage).Copy: Error finishing the upload session: %w", err)
	}

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
