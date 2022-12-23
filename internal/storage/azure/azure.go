// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package azure

import (
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
)

type azureBlobStorage struct {
	*storage.StorageBackend
}

// Config contains values that define the configuration of an Azure Blob Storage.
type Config struct {
	AccountName       string
	ContainerName     string
	PrimaryAccountKey string
}

// NewStorageBackend creates and initializes a new S3/Minio storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	storage := azureBlobStorage{}
	return &storage, nil
}

// Name returns the name of the storage backend
func (v *azureBlobStorage) Name() string {
	return "Azure"
}

// Copy copies the given file to the storage backend.
func (b *azureBlobStorage) Copy(file string) error {
	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the S3/Minio storage backend.
func (b *azureBlobStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	return nil, nil
}
