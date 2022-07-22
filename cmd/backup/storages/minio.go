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

	a := &GenericStorage{}
	r := &S3Storage{a, mc}
	a.backupRetentionDays = c.BackupRetentionDays
	a.backupPruningPrefix = c.BackupPruningPrefix
	a.logger = l
	a.config = c
	a.Storage = r
	return r, nil
}

func (s3 *S3Storage) Copy(file string) error {
	s3.logger.Infof("copyArchive->s3stg: Beginning...")
	_, name := path.Split(file)
	if _, err := s3.client.FPutObject(context.Background(), s3.config.AwsS3BucketName, filepath.Join(s3.config.AwsS3Path, name), file, minio.PutObjectOptions{
		ContentType:  "application/tar+gzip",
		StorageClass: s3.config.AwsStorageClass,
	}); err != nil {
		return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
	}
	s3.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`.", file, s3.config.AwsS3BucketName)

	return nil
}

func (s3 *S3Storage) Prune(deadline time.Time) (*t.StorageStats, error) {
	candidates := s3.client.ListObjects(context.Background(), s3.config.AwsS3BucketName, minio.ListObjectsOptions{
		WithMetadata: true,
		Prefix:       filepath.Join(s3.config.AwsS3Path, s3.backupPruningPrefix),
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

	s3.doPrune(len(matches), lenCandidates, "remote backup(s)", func() error {
		objectsCh := make(chan minio.ObjectInfo)
		go func() {
			for _, match := range matches {
				objectsCh <- match
			}
			close(objectsCh)
		}()
		errChan := s3.client.RemoveObjects(context.Background(), s3.config.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
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
