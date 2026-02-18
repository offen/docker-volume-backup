// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/offen/docker-volume-backup/internal/errwrap"
	"github.com/offen/docker-volume-backup/internal/storage"
)

type s3Storage struct {
	*storage.StorageBackend
	client       *minio.Client
	bucket       string
	storageClass string
	partSize     int64
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
	PartSize         int64
	CACert           *x509.Certificate
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
		return nil, errwrap.Wrap(nil, "AWS_S3_BUCKET_NAME is defined, but no credentials were provided")
	}

	options := minio.Options{
		Creds:  creds,
		Secure: opts.EndpointProto == "https",
	}

	transport, err := minio.DefaultTransport(true)
	if err != nil {
		return nil, errwrap.Wrap(err, "failed to create default minio transport")
	}

	if opts.EndpointInsecure {
		if !options.Secure {
			return nil, errwrap.Wrap(nil, "AWS_ENDPOINT_INSECURE = true is only meaningful for https")
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
	} else if opts.CACert != nil {
		if transport.TLSClientConfig.RootCAs == nil {
			transport.TLSClientConfig.RootCAs = x509.NewCertPool()
		}
		transport.TLSClientConfig.RootCAs.AddCert(opts.CACert)
	}
	options.Transport = transport

	mc, err := minio.New(opts.Endpoint, &options)
	if err != nil {
		return nil, errwrap.Wrap(err, "error setting up minio client")
	}

	return &s3Storage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.RemotePath,
			Log:             logFunc,
		},
		client:       mc,
		bucket:       opts.BucketName,
		storageClass: opts.StorageClass,
		partSize:     opts.PartSize,
	}, nil
}

// Name returns the name of the storage backend
func (v *s3Storage) Name() string {
	return "S3"
}

// Copy copies the given file to the S3/Minio storage backend.
func (b *s3Storage) Copy(file string) error {
	_, name := path.Split(file)
	putObjectOptions := minio.PutObjectOptions{
		ContentType:    "application/tar+gzip",
		StorageClass:   b.storageClass,
		SendContentMd5: true,
	}

	if b.partSize > 0 {
		srcFileInfo, err := os.Stat(file)
		if err != nil {
			return errwrap.Wrap(err, "error reading the local file")
		}

		_, partSize, _, err := minio.OptimalPartInfo(srcFileInfo.Size(), uint64(b.partSize*1024*1024))
		if err != nil {
			return errwrap.Wrap(err, "error computing the optimal s3 part size")
		}

		putObjectOptions.PartSize = uint64(partSize)
	}

	if _, err := b.client.FPutObject(context.Background(), b.bucket, path.Join(b.DestinationPath, name), file, putObjectOptions); err != nil {
		if errResp := minio.ToErrorResponse(err); errResp.Message != "" {
			return errwrap.Wrap(
				nil,
				fmt.Sprintf(
					"error uploading backup to remote storage: [Message]: '%s', [Code]: %s, [StatusCode]: %d",
					errResp.Message,
					errResp.Code,
					errResp.StatusCode,
				),
			)
		}
		return errwrap.Wrap(err, "error uploading backup to remote storage")
	}

	b.Log(storage.LogLevelInfo, b.Name(), "Uploaded a copy of backup `%s` to bucket `%s`.", file, b.bucket)

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the S3/Minio storage backend.
func (b *s3Storage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	candidates := b.client.ListObjects(context.Background(), b.bucket, minio.ListObjectsOptions{
		Prefix:    path.Join(b.DestinationPath, pruningPrefix),
		Recursive: true,
	})

	var matches []minio.ObjectInfo
	var lenCandidates int
	for candidate := range candidates {
		lenCandidates++
		if candidate.Err != nil {
			return nil, errwrap.Wrap(
				candidate.Err,
				"error looking up candidates from remote storage",
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

	pruneErr := b.DoPrune(b.Name(), len(matches), lenCandidates, deadline, func() error {
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
			return errors.Join(removeErrors...)
		}
		return nil
	})

	return stats, pruneErr
}
