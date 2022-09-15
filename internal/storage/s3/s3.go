// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

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
	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/offen/docker-volume-backup/internal/utilities"
)

type s3Storage struct {
	*storage.StorageBackend
	client       *minio.Client
	bucket       string
	storageClass string
}

// Config contains values that define the configuration of a S3 backend.
type Config struct {
	Endpoint         string
	AccessKeyID      string
	SecretAccessKey  string
	IamRoleEndpoint  string
	EndpointProto    string
	EndpointInsecure bool
	RemotePath       string
	BucketName       string
	StorageClass     string
}

// NewStorageBackend creates and initializes a new S3/Minio storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {

	var creds *credentials.Credentials
	if opts.AccessKeyID != "" && opts.SecretAccessKey != "" {
		creds = credentials.NewStaticV4(
			opts.AccessKeyID,
			opts.SecretAccessKey,
			"",
		)
	} else if opts.IamRoleEndpoint != "" {
		creds = credentials.NewIAM(opts.IamRoleEndpoint)
	} else {
		return nil, errors.New("NewStorageBackend: AWS_S3_BUCKET_NAME is defined, but no credentials were provided")
	}

	options := minio.Options{
		Creds:  creds,
		Secure: opts.EndpointProto == "https",
	}

	if opts.EndpointInsecure {
		if !options.Secure {
			return nil, errors.New("NewStorageBackend: AWS_ENDPOINT_INSECURE = true is only meaningful for https")
		}

		transport, err := minio.DefaultTransport(true)
		if err != nil {
			return nil, fmt.Errorf("NewStorageBackend: failed to create default minio transport: %w", err)
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
		options.Transport = transport
	}

	mc, err := minio.New(opts.Endpoint, &options)
	if err != nil {
		return nil, fmt.Errorf("NewStorageBackend: error setting up minio client: %w", err)
	}

	return &s3Storage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.RemotePath,
			Log:             logFunc,
		},
		client:       mc,
		bucket:       opts.BucketName,
		storageClass: opts.StorageClass,
	}, nil
}

// Name returns the name of the storage backend
func (v *s3Storage) Name() string {
	return "S3"
}

// Copy copies the given file to the S3/Minio storage backend.
func (b *s3Storage) Copy(file string) error {
	_, name := path.Split(file)

	if _, err := b.client.FPutObject(context.Background(), b.bucket, filepath.Join(b.DestinationPath, name), file, minio.PutObjectOptions{
		ContentType:  "application/tar+gzip",
		StorageClass: b.storageClass,
	}); err != nil {
		errResp := minio.ToErrorResponse(err)
		return fmt.Errorf("(*s3Storage).Copy: error uploading backup to remote storage: [Message]: '%s', [Code]: %s, [StatusCode]: %d", errResp.Message, errResp.Code, errResp.StatusCode)
	}
	b.Log(storage.LogLevelInfo, b.Name(), "Uploaded a copy of backup `%s` to bucket `%s`.", file, b.bucket)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the S3/Minio storage backend.
func (b *s3Storage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates := b.client.ListObjects(context.Background(), b.bucket, minio.ListObjectsOptions{
		WithMetadata: true,
		Prefix:       filepath.Join(b.DestinationPath, pruningPrefix),
		Recursive:    true,
	})

	var matches []minio.ObjectInfo
	var lenCandidates int
	for candidate := range candidates {
		lenCandidates++
		if candidate.Err != nil {
			return nil, fmt.Errorf(
				"(*s3Storage).Prune: Error looking up candidates from remote storage! %w",
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

	if err := b.DoPrune(b.Name(), len(matches), lenCandidates, "remote backup(s)", func() error {
		objectsCh := make(chan minio.ObjectInfo)
		go func() {
			for _, match := range matches {
				objectsCh <- match
			}
			close(objectsCh)
		}()
		errChan := b.client.RemoveObjects(context.Background(), b.bucket, objectsCh, minio.RemoveObjectsOptions{})
		var removeErrors []error
		for result := range errChan {
			if result.Err != nil {
				removeErrors = append(removeErrors, result.Err)
			}
		}
		if len(removeErrors) != 0 {
			return utilities.Join(removeErrors...)
		}
		return nil
	}); err != nil {
		return stats, err
	}

	return stats, nil
}
