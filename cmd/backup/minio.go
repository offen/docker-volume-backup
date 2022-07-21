package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
)

type MinioHelper struct {
	*AbstractHelper
	client *minio.Client
}

func newMinioHelper(client *minio.Client) *MinioHelper {
	a := &AbstractHelper{}
	r := &MinioHelper{a, client}
	a.Helper = r
	return r
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
