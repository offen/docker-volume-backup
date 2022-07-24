package s3

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	strg "github.com/offen/docker-volume-backup/internal/storage"
	t "github.com/offen/docker-volume-backup/internal/types"
	u "github.com/offen/docker-volume-backup/internal/utilities"
	"github.com/sirupsen/logrus"
)

type S3Storage struct {
	*strg.StorageBackend
	client *minio.Client
}

// Specific init procedure for the S3/Minio storage provider.
func InitS3(c *t.Config, l *logrus.Logger, s *t.Stats) (*strg.StorageBackend, error) {
	var creds *credentials.Credentials
	if c.AwsAccessKeyID != "" && c.AwsSecretAccessKey != "" {
		creds = credentials.NewStaticV4(
			c.AwsAccessKeyID,
			c.AwsSecretAccessKey,
			"",
		)
	} else if c.AwsIamRoleEndpoint != "" {
		creds = credentials.NewIAM(c.AwsIamRoleEndpoint)
	} else {
		return nil, errors.New("newScript: AWS_S3_BUCKET_NAME is defined, but no credentials were provided")
	}

	options := minio.Options{
		Creds:  creds,
		Secure: c.AwsEndpointProto == "https",
	}

	if c.AwsEndpointInsecure {
		if !options.Secure {
			return nil, errors.New("newScript: AWS_ENDPOINT_INSECURE = true is only meaningful for https")
		}

		transport, err := minio.DefaultTransport(true)
		if err != nil {
			return nil, fmt.Errorf("newScript: failed to create default minio transport")
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
		options.Transport = transport
	}

	mc, err := minio.New(c.AwsEndpoint, &options)
	if err != nil {
		return nil, fmt.Errorf("newScript: error setting up minio client: %w", err)
	}

	a := &strg.StorageBackend{
		Storage: &S3Storage{},
		Name:    "S3",
		Logger:  l,
		Config:  c,
		Stats:   s,
	}
	r := &S3Storage{a, mc}
	a.Storage = r
	return a, nil
}

// Specific copy function for the S3/Minio storage provider.
func (stg *S3Storage) Copy(file string) error {
	_, name := path.Split(file)

	if _, err := stg.client.FPutObject(context.Background(), stg.Config.AwsS3BucketName, filepath.Join(stg.Config.AwsS3Path, name), file, minio.PutObjectOptions{
		ContentType:  "application/tar+gzip",
		StorageClass: stg.Config.AwsStorageClass,
	}); err != nil {
		return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
	}
	stg.Logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`.", file, stg.Config.AwsS3BucketName)

	return nil
}

// Specific prune function for the S3/Minio storage provider.
func (stg *S3Storage) Prune(deadline time.Time) error {
	candidates := stg.client.ListObjects(context.Background(), stg.Config.AwsS3BucketName, minio.ListObjectsOptions{
		WithMetadata: true,
		Prefix:       filepath.Join(stg.Config.AwsS3Path, stg.Config.BackupPruningPrefix),
		Recursive:    true,
	})

	var matches []minio.ObjectInfo
	var lenCandidates int
	for candidate := range candidates {
		lenCandidates++
		if candidate.Err != nil {
			return fmt.Errorf(
				"pruneBackups: error looking up candidates from remote storage: %w",
				candidate.Err,
			)
		}
		if candidate.LastModified.Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stg.Stats.Storages.S3 = t.StorageStats{
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
		errChan := stg.client.RemoveObjects(context.Background(), stg.Config.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
		var removeErrors []error
		for result := range errChan {
			if result.Err != nil {
				removeErrors = append(removeErrors, result.Err)
			}
		}
		if len(removeErrors) != 0 {
			return u.Join(removeErrors...)
		}
		return nil
	})

	return nil
}
