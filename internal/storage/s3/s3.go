package s3

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/offen/docker-volume-backup/internal/storage"
	utilites "github.com/offen/docker-volume-backup/internal/utilities"
)

type s3Storage struct {
	*storage.StorageBackend
	client       *minio.Client
	bucket       string
	storageClass string
}

// NewStorageBackend creates and initializes a new S3/Minio storage backend.
func NewStorageBackend(endpoint string, accessKeyId string, secretAccessKey string, iamRoleEndpoint string, endpointProto string, endpointInsecure bool,
	remotePath string, bucket string, storageClass string, logFunc storage.LogFuncDef) (storage.Backend, error) {

	var creds *credentials.Credentials
	if accessKeyId != "" && secretAccessKey != "" {
		creds = credentials.NewStaticV4(
			accessKeyId,
			secretAccessKey,
			"",
		)
	} else if iamRoleEndpoint != "" {
		creds = credentials.NewIAM(iamRoleEndpoint)
	} else {
		return nil, errors.New("newScript: AWS_S3_BUCKET_NAME is defined, but no credentials were provided")
	}

	options := minio.Options{
		Creds:  creds,
		Secure: endpointProto == "https",
	}

	if endpointInsecure {
		if !options.Secure {
			return nil, errors.New("newScript: AWS_ENDPOINT_INSECURE = true is only meaningful for https")
		}

		transport, err := minio.DefaultTransport(true)
		if err != nil {
			return nil, logFunc(storage.ERROR, "S3", "NewScript: failed to create default minio transport")
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
		options.Transport = transport
	}

	mc, err := minio.New(endpoint, &options)
	if err != nil {
		return nil, logFunc(storage.ERROR, "S3", "NewScript: error setting up minio client: %w", err)
	}

	strgBackend := &storage.StorageBackend{
		Backend:         &s3Storage{},
		DestinationPath: remotePath,
		Log:             logFunc,
	}
	sshBackend := &s3Storage{
		StorageBackend: strgBackend,
		client:         mc,
		bucket:         bucket,
		storageClass:   storageClass,
	}
	strgBackend.Backend = sshBackend
	return strgBackend, nil
}

// Name returns the name of the storage backend
func (stg *s3Storage) Name() string {
	return "S3"
}

// Copy copies the given file to the S3/Minio storage backend.
func (stg *s3Storage) Copy(file string) error {
	_, name := path.Split(file)

	if _, err := stg.client.FPutObject(context.Background(), stg.bucket, filepath.Join(stg.DestinationPath, name), file, minio.PutObjectOptions{
		ContentType:  "application/tar+gzip",
		StorageClass: stg.storageClass,
	}); err != nil {
		errResp := minio.ToErrorResponse(err)
		return stg.Log(storage.ERROR, stg.Name(), "Copy: error uploading backup to remote storage: [Message]: '%s', [Code]: %s, [StatusCode]: %d", errResp.Message, errResp.Code, errResp.StatusCode)
	}
	stg.Log(storage.INFO, stg.Name(), "Uploaded a copy of backup `%s` to bucket `%s`.", file, stg.bucket)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the S3/Minio storage backend.
func (stg *s3Storage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates := stg.client.ListObjects(context.Background(), stg.bucket, minio.ListObjectsOptions{
		WithMetadata: true,
		Prefix:       filepath.Join(stg.DestinationPath, pruningPrefix),
		Recursive:    true,
	})

	var matches []minio.ObjectInfo
	var lenCandidates int
	for candidate := range candidates {
		lenCandidates++
		if candidate.Err != nil {
			return nil, stg.Log(storage.ERROR, stg.Name(),
				"Prune: Error looking up candidates from remote storage! %w",
				candidate.Err,
			)
		}
		if candidate.LastModified.Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	stg.DoPrune(len(matches), lenCandidates, "remote backup(s)", func() error {
		objectsCh := make(chan minio.ObjectInfo)
		go func() {
			for _, match := range matches {
				objectsCh <- match
			}
			close(objectsCh)
		}()
		errChan := stg.client.RemoveObjects(context.Background(), stg.bucket, objectsCh, minio.RemoveObjectsOptions{})
		var removeErrors []error
		for result := range errChan {
			if result.Err != nil {
				removeErrors = append(removeErrors, result.Err)
			}
		}
		if len(removeErrors) != 0 {
			return utilites.Join(removeErrors...)
		}
		return nil
	})

	return stats, nil
}
