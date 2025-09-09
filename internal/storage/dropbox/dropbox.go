package dropbox

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"github.com/offen/docker-volume-backup/internal/errwrap"
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
	OAuth2Endpoint   string
	RefreshToken     string
	AppKey           string
	AppSecret        string
	RemotePath       string
	ConcurrencyLevel int
}

// NewStorageBackend creates and initializes a new Dropbox storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	tokenUrl, _ := url.JoinPath(opts.OAuth2Endpoint, "oauth2/token")

	conf := &oauth2.Config{
		ClientID:     opts.AppKey,
		ClientSecret: opts.AppSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenUrl,
		},
	}

	logFunc(storage.LogLevelInfo, "Dropbox", "Fetching fresh access token for Dropbox storage backend.")
	tkSource := conf.TokenSource(context.Background(), &oauth2.Token{RefreshToken: opts.RefreshToken})
	token, err := tkSource.Token()
	if err != nil {
		return nil, errwrap.Wrap(err, "error refreshing token")
	}

	dbxConfig := dropbox.Config{
		Token: token.AccessToken,
	}

	if opts.Endpoint != "https://api.dropbox.com/" {
		dbxConfig.URLGenerator = func(hostType string, namespace string, route string) string {
			return fmt.Sprintf("%s/%d/%s/%s", opts.Endpoint, 2, namespace, route)
		}
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
func (b *dropboxStorage) Copy(file string) (returnErr error) {
	_, name := path.Split(file)

	folderArg := files.NewCreateFolderArg(b.DestinationPath)
	if _, err := b.client.CreateFolderV2(folderArg); err != nil {
		switch err := err.(type) {
		case files.CreateFolderV2APIError:
			if err.EndpointError.Path.Tag != files.WriteErrorConflict {
				returnErr = errwrap.Wrap(err, fmt.Sprintf("error creating directory '%s'", b.DestinationPath))
				return
			}
			b.Log(storage.LogLevelInfo, b.Name(), "Destination path '%s' already exists, no new directory required.", b.DestinationPath)
		default:
			returnErr = errwrap.Wrap(err, fmt.Sprintf("error creating directory '%s'", b.DestinationPath))
			return
		}
	}

	r, err := os.Open(file)
	if err != nil {
		returnErr = errwrap.Wrap(err, "error opening the file to be uploaded")
		return
	}
	defer func() {
		returnErr = r.Close()
	}()

	// Start new upload session and get session id
	b.Log(storage.LogLevelInfo, b.Name(), "Starting upload session for backup '%s' at path '%s'.", file, b.DestinationPath)

	var sessionId string
	uploadSessionStartArg := files.NewUploadSessionStartArg()
	uploadSessionStartArg.SessionType = &files.UploadSessionType{Tagged: dropbox.Tagged{Tag: files.UploadSessionTypeConcurrent}}
	if res, err := b.client.UploadSessionStart(uploadSessionStartArg, nil); err != nil {
		returnErr = errwrap.Wrap(err, "error starting the upload session")
		return
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
				errorChn <- errwrap.Wrap(err, "error reading the file to be uploaded")
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
				errorChn <- errwrap.Wrap(err, "error appending the file to the upload session")
				return
			}
		}()
	}

	// Finish the upload session, commit the file (no new data added)

	_, err = b.client.UploadSessionFinish(
		files.NewUploadSessionFinishArg(
			files.NewUploadSessionCursor(sessionId, 0),
			files.NewCommitInfo(path.Join(b.DestinationPath, name)),
		), nil)
	if err != nil {
		returnErr = errwrap.Wrap(err, "error finishing the upload session")
		return
	}

	b.Log(storage.LogLevelInfo, b.Name(), "Uploaded a copy of backup '%s' at path '%s'.", file, b.DestinationPath)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the Dropbox storage backend.
func (b *dropboxStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	var entries []files.IsMetadata
	res, err := b.client.ListFolder(files.NewListFolderArg(b.DestinationPath))
	if err != nil {
		return nil, errwrap.Wrap(err, "error looking up candidates from remote storage")
	}
	entries = append(entries, res.Entries...)

	for res.HasMore {
		res, err = b.client.ListFolderContinue(files.NewListFolderContinueArg(res.Cursor))
		if err != nil {
			return nil, errwrap.Wrap(err, "error looking up candidates from remote storage")
		}
		entries = append(entries, res.Entries...)
	}

	var matches []*files.FileMetadata
	var lenCandidates int
	for _, candidate := range entries {
		switch candidate := candidate.(type) {
		case *files.FileMetadata:
			if !strings.HasPrefix(candidate.Name, pruningPrefix) {
				continue
			}
			lenCandidates++
			if candidate.ServerModified.Before(deadline) {
				matches = append(matches, candidate)
			}
		default:
			continue
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	pruneErr := b.DoPrune(b.Name(), len(matches), lenCandidates, deadline, func() error {
		for _, match := range matches {
			if _, err := b.client.DeleteV2(files.NewDeleteArg(path.Join(b.DestinationPath, match.Name))); err != nil {
				return errwrap.Wrap(err, "error removing file from Dropbox storage")
			}
		}
		return nil
	})

	return stats, pruneErr
}
