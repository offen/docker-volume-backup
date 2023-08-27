// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"
)

// Config holds all configuration values that are expected to be set
// by users.
type Config struct {
	AwsS3BucketName               string
	AwsS3Path                     string
	AwsEndpoint                   string `default:"s3.amazonaws.com"`
	AwsEndpointProto              string
	AwsEndpointInsecure           bool
	AwsEndpointCACert             CertDecoder
	AwsStorageClass               string
	AwsAccessKeyID                string
	AwsSecretAccessKey            string
	AwsIamRoleEndpoint            string
	AwsPartSize                   int64
	BackupCompression             CompressionType `default:"gz"`
	BackupSources                 string          `default:"/backup"`
	BackupFilename                string          `default:"backup-%Y-%m-%dT%H-%M-%S.{{ .Extension }}"`
	BackupFilenameExpand          bool
	BackupLatestSymlink           string
	BackupArchive                 string        `default:"/archive"`
	BackupRetentionDays           int32         `default:"-1"`
	BackupPruningLeeway           time.Duration `default:"1m"`
	BackupPruningPrefix           string
	BackupStopContainerLabel      string `default:"true"`
	BackupFromSnapshot            bool
	BackupExcludeRegexp           RegexpDecoder
	BackupSkipBackendsFromPrune   []string
	GpgPassphrase                 string
	NotificationURLs              []string
	NotificationLevel             string `default:"error"`
	EmailNotificationRecipient    string
	EmailNotificationSender       string `default:"noreply@nohost"`
	EmailSMTPHost                 string
	EmailSMTPPort                 int `default:"587"`
	EmailSMTPUsername             string
	EmailSMTPPassword             string
	WebdavUrl                     string
	WebdavUrlInsecure             bool
	WebdavPath                    string `default:"/"`
	WebdavUsername                string
	WebdavPassword                string
	SSHHostName                   string
	SSHPort                       string `default:"22"`
	SSHUser                       string
	SSHPassword                   string
	SSHIdentityFile               string `default:"/root/.ssh/id_rsa"`
	SSHIdentityPassphrase         string
	SSHRemotePath                 string
	ExecLabel                     string
	ExecForwardOutput             bool
	LockTimeout                   time.Duration `default:"60m"`
	AzureStorageAccountName       string
	AzureStoragePrimaryAccountKey string
	AzureStorageContainerName     string
	AzureStoragePath              string
	AzureStorageEndpoint          string `default:"https://{{ .AccountName }}.blob.core.windows.net/"`
	DropboxEndpoint               string `default:"https://api.dropbox.com/"`
	DropboxOAuth2Endpoint         string `default:"https://api.dropbox.com/"`
	DropboxRefreshToken           string
	DropboxAppKey                 string
	DropboxAppSecret              string
	DropboxRemotePath             string
	DropboxConcurrencyLevel       NaturalNumber `default:"6"`
}

type CompressionType string

func (c *CompressionType) UnmarshalText(text []byte) error {
	v := string(text)
	switch v {
	case "gz", "zst":
		*c = CompressionType(v)
		return nil
	default:
		return fmt.Errorf("config: error decoding compression type %s", v)
	}
}

func (c *CompressionType) String() string {
	return string(*c)
}

type CertDecoder struct {
	Cert *x509.Certificate
}

func (c *CertDecoder) UnmarshalText(text []byte) error {
	v := string(text)
	if v == "" {
		return nil
	}
	content, err := os.ReadFile(v)
	if err != nil {
		content = []byte(v)
	}
	block, _ := pem.Decode(content)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("config: error parsing certificate: %w", err)
	}
	*c = CertDecoder{Cert: cert}
	return nil
}

type RegexpDecoder struct {
	Re *regexp.Regexp
}

func (r *RegexpDecoder) UnmarshalText(text []byte) error {
	v := string(text)
	if v == "" {
		return nil
	}
	re, err := regexp.Compile(v)
	if err != nil {
		return fmt.Errorf("config: error compiling given regexp `%s`: %w", v, err)
	}
	*r = RegexpDecoder{Re: re}
	return nil
}

type NaturalNumber int

func (n *NaturalNumber) UnmarshalText(text []byte) error {
	v := string(text)
	asInt, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("config: error converting %s to int", v)
	}
	if asInt <= 0 {
		return fmt.Errorf("config: expected a natural number, got %d", asInt)
	}
	*n = NaturalNumber(asInt)
	return nil
}

func (n *NaturalNumber) Int() int {
	return int(*n)
}
