package storages

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	t "github.com/offen/docker-volume-backup/cmd/backup/types"
	u "github.com/offen/docker-volume-backup/cmd/backup/utilities"
	"github.com/sirupsen/logrus"
)

type S3Storage struct {
	*GenericStorage
	client *minio.Client
}

// Specific init procedure for the S3/Minio storage provider.
func InitS3(c *t.Config, l *logrus.Logger) (*S3Storage, error) {
	if c.AwsS3BucketName == "" {
		return nil, nil
	}

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

	a := &GenericStorage{&S3Storage{}, l, c}
	r := &S3Storage{a, mc}
	return r, nil
}

// Specific copy function for the S3/Minio storage provider.
func (stg *S3Storage) copy(file string) error {
	_, name := path.Split(file)
	if _, err := stg.client.FPutObject(context.Background(), stg.config.AwsS3BucketName, filepath.Join(stg.config.AwsS3Path, name), file, minio.PutObjectOptions{
		ContentType:  "application/tar+gzip",
		StorageClass: stg.config.AwsStorageClass,
	}); err != nil {
		return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
	}
	stg.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`.", file, stg.config.AwsS3BucketName)

	return nil
}

// Specific prune function for the S3/Minio storage provider.
func (stg *S3Storage) prune(deadline time.Time) (*t.StorageStats, error) {
	candidates := stg.client.ListObjects(context.Background(), stg.config.AwsS3BucketName, minio.ListObjectsOptions{
		WithMetadata: true,
		Prefix:       filepath.Join(stg.config.AwsS3Path, stg.config.BackupPruningPrefix),
		Recursive:    true,
	})

	var matches []minio.ObjectInfo
	var lenCandidates int
	for candidate := range candidates {
		lenCandidates++
		if candidate.Err != nil {
			return nil, fmt.Errorf(
				"pruneBackups: error looking up candidates from remote storage: %w",
				candidate.Err,
			)
		}
		if candidate.LastModified.Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stats := t.StorageStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	stg.doPrune(len(matches), lenCandidates, "remote backup(s)", func() error {
		objectsCh := make(chan minio.ObjectInfo)
		go func() {
			for _, match := range matches {
				objectsCh <- match
			}
			close(objectsCh)
		}()
		errChan := stg.client.RemoveObjects(context.Background(), stg.config.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
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

	return &stats, nil
}
