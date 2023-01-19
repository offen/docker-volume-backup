// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package azure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
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
	RemotePath        string
}

// NewStorageBackend creates and initializes a new Azure Blob Storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) (storage.Backend, error) {
	endpointTemplate, err := template.New("endpoint").Parse(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("NewStorageBackend: error parsing endpoint template: %w", err)
	}
	var ep bytes.Buffer
	if err := endpointTemplate.Execute(&ep, opts); err != nil {
		return nil, fmt.Errorf("NewStorageBackend: error executing endpoint template: %w", err)
	}
	normalizedEndpoint := fmt.Sprintf("%s/", strings.TrimSuffix(ep.String(), "/"))

	var client *azblob.Client
	if opts.PrimaryAccountKey != "" {
		cred, err := azblob.NewSharedKeyCredential(opts.AccountName, opts.PrimaryAccountKey)
		if err != nil {
			return nil, fmt.Errorf("NewStorageBackend: error creating shared key Azure credential: %w", err)
		}

		client, err = azblob.NewClientWithSharedKeyCredential(normalizedEndpoint, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("NewStorageBackend: error creating Azure client: %w", err)
		}
	} else {
		cred, err := azidentity.NewManagedIdentityCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("NewStorageBackend: error creating managed identity credential: %w", err)
		}
		client, err = azblob.NewClient(normalizedEndpoint, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("NewStorageBackend: error creating Azure client: %w", err)
		}
	}

	storage := azureBlobStorage{
		client:        client,
		containerName: opts.ContainerName,
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.RemotePath,
			Log:             logFunc,
		},
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
	_, err = b.client.UploadStream(
		context.Background(),
		b.containerName,
		filepath.Join(b.DestinationPath, filepath.Base(file)),
		fileReader,
		nil,
	)
	if err != nil {
		return fmt.Errorf("(*azureBlobStorage).Copy: error uploading file %s: %w", file, err)
	}
	return nil
}

// Prune rotates away backups according to the configuration and provided
// deadline for the Azure Blob storage backend.
func (b *azureBlobStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	lookupPrefix := filepath.Join(b.DestinationPath, pruningPrefix)
	pager := b.client.NewListBlobsFlatPager(b.containerName, &container.ListBlobsFlatOptions{
		Prefix: &lookupPrefix,
	})
	var matches []string
	var totalCount uint
	for pager.More() {
		resp, err := pager.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("(*azureBlobStorage).Prune: error paging over blobs: %w", err)
		}
		for _, v := range resp.Segment.BlobItems {
			totalCount++
			if v.Properties.LastModified.Before(deadline) {
				matches = append(matches, *v.Name)
			}
		}
	}

	stats := storage.PruneStats{
		Total:  totalCount,
		Pruned: uint(len(matches)),
	}

	if err := b.DoPrune(b.Name(), len(matches), int(totalCount), "Azure Blob Storage backup(s)", func() error {
		wg := sync.WaitGroup{}
		wg.Add(len(matches))
		var errs []error

		for _, match := range matches {
			name := match
			go func() {
				_, err := b.client.DeleteBlob(context.Background(), b.containerName, name, nil)
				if err != nil {
					errs = append(errs, err)
				}
				wg.Done()
			}()
		}
		wg.Wait()
		if len(errs) != 0 {
			return errors.Join(errs...)
		}
		return nil
	}); err != nil {
		return &stats, err
	}

	return &stats, nil
}
