package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioHelper struct {
	*AbstractHelper
	client *minio.Client
}

func newMinioHelper(s *script) (*MinioHelper, error) {
	var creds *credentials.Credentials
	if s.c.AwsAccessKeyID != "" && s.c.AwsSecretAccessKey != "" {
		creds = credentials.NewStaticV4(
			s.c.AwsAccessKeyID,
			s.c.AwsSecretAccessKey,
			"",
		)
	} else if s.c.AwsIamRoleEndpoint != "" {
		creds = credentials.NewIAM(s.c.AwsIamRoleEndpoint)
	} else {
		return nil, errors.New("newScript: AWS_S3_BUCKET_NAME is defined, but no credentials were provided")
	}

	options := minio.Options{
		Creds:  creds,
		Secure: s.c.AwsEndpointProto == "https",
	}

	if s.c.AwsEndpointInsecure {
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

	mc, err := minio.New(s.c.AwsEndpoint, &options)
	if err != nil {
		return nil, fmt.Errorf("newScript: error setting up minio client: %w", err)
	}

	a := &AbstractHelper{}
	r := &MinioHelper{a, mc}
	a.Helper = r
	return r, nil
}

func (helper *MinioHelper) copyArchive(s *script, name string) error {
	if _, err := helper.client.FPutObject(context.Background(), s.c.AwsS3BucketName, filepath.Join(s.c.AwsS3Path, name), s.file, minio.PutObjectOptions{
		ContentType:  "application/tar+gzip",
		StorageClass: s.c.AwsStorageClass,
	}); err != nil {
		return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
	}
	s.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`.", s.file, s.c.AwsS3BucketName)

	return nil
}

func (helper *MinioHelper) pruneBackups(s *script, deadline time.Time) error {
	candidates := helper.client.ListObjects(context.Background(), s.c.AwsS3BucketName, minio.ListObjectsOptions{
		WithMetadata: true,
		Prefix:       filepath.Join(s.c.AwsS3Path, s.c.BackupPruningPrefix),
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

	s.stats.Storages.S3 = StorageStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	doPrune(s, len(matches), lenCandidates, "remote backup(s)", func() error {
		objectsCh := make(chan minio.ObjectInfo)
		go func() {
			for _, match := range matches {
				objectsCh <- match
			}
			close(objectsCh)
		}()
		errChan := helper.client.RemoveObjects(context.Background(), s.c.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
		var removeErrors []error
		for result := range errChan {
			if result.Err != nil {
				removeErrors = append(removeErrors, result.Err)
			}
		}
		if len(removeErrors) != 0 {
			return join(removeErrors...)
		}
		return nil
	})

	return nil
}
