// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package azure

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/offen/docker-volume-backup/internal/storage"
)

type azureBlobStorage struct {
	*storage.StorageBackend
	client        *azblob.Client
	containerName string
}

// Config contains values that define the configuration of an Azure Blob Storage.
type Config struct {
	AccountName       string
	ContainerName     string
	PrimaryAccountKey string
	Endpoint          string
}

// NewStorageBackend creates and initializes a new Azure Blob Storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	cred, err := azblob.NewSharedKeyCredential(opts.AccountName, opts.PrimaryAccountKey)
	if err != nil {
		return nil, fmt.Errorf("NewStorageBackend: error creating shared Azure credential: %w", err)
	}
	client, err := azblob.NewClientWithSharedKeyCredential(fmt.Sprintf(opts.Endpoint, opts.AccountName), cred, nil)
	if err != nil {
		return nil, fmt.Errorf("NewStorageBackend: error creating Azure client: %w", err)
	}
	storage := azureBlobStorage{
		client:        client,
		containerName: opts.ContainerName,
	}
	return &storage, nil
}

// Name returns the name of the storage backend
func (b *azureBlobStorage) Name() string {
	return "Azure"
}

// Copy copies the given file to the storage backend.
func (b *azureBlobStorage) Copy(file string) error {
	fileReader, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("(*azureBlobStorage).Copy: error opening file %s: %w", file, err)
	}
	_, err = b.client.UploadStream(context.TODO(),
		b.containerName,
		file,
		fileReader,
		nil,
	)
	if err != nil {
		return fmt.Errorf("(*azureBlobStorage).Copy: error uploading file %s: %w", file, err)
	}
	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the S3/Minio storage backend.
func (b *azureBlobStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	return nil, nil
}
