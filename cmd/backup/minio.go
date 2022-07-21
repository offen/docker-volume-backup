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
	s      *script
}

func newMinioHelper(s *script) (*MinioHelper, error) {
	if s.c.AwsS3BucketName == "" {
		return nil, nil
	}

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
	r := &MinioHelper{a, mc, s}
	a.Helper = r
	return r, nil
}

func (helper *MinioHelper) copyArchive(name string) error {
	if _, err := helper.client.FPutObject(context.Background(), helper.s.c.AwsS3BucketName, filepath.Join(helper.s.c.AwsS3Path, name), helper.s.file, minio.PutObjectOptions{
		ContentType:  "application/tar+gzip",
		StorageClass: helper.s.c.AwsStorageClass,
	}); err != nil {
		return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
	}
	helper.s.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`.", helper.s.file, helper.s.c.AwsS3BucketName)

	return nil
}

func (helper *MinioHelper) pruneBackups(deadline time.Time) error {
	candidates := helper.client.ListObjects(context.Background(), helper.s.c.AwsS3BucketName, minio.ListObjectsOptions{
		WithMetadata: true,
		Prefix:       filepath.Join(helper.s.c.AwsS3Path, helper.s.c.BackupPruningPrefix),
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

	helper.s.stats.Storages.S3 = StorageStats{
		Total:  uint(lenCandidates),
		Pruned: uint(len(matches)),
	}

	doPrune(helper.s, len(matches), lenCandidates, "remote backup(s)", func() error {
		objectsCh := make(chan minio.ObjectInfo)
		go func() {
			for _, match := range matches {
				objectsCh <- match
			}
			close(objectsCh)
		}()
		errChan := helper.client.RemoveObjects(context.Background(), helper.s.c.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
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
