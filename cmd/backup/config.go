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
	AwsS3BucketName               string `env:"AWS_S3_BUCKET_NAME"`
	AwsS3Path                     string `env:"AWS_S3_PATH"`
	AwsEndpoint                   string `envDefault:"s3.amazonaws.com"`
	AwsEndpointProto              string `envDefault:"https"`
	AwsEndpointInsecure           bool
	AwsEndpointCACert             CertDecoder `env:"AWS_ENDPOINT_CA_CERT"`
	AwsStorageClass               string
	AwsAccessKeyID                string `env:"AWS_ACCESS_KEY_ID"`
	AwsAccessKeyIDFile            string `env:"AWS_ACCESS_KEY_ID_FILE,file"`
	AwsSecretAccessKey            string `env:"AWS_SECRET_ACCESS_KEY"`
	AwsSecretAccessKeyFile        string `env:"AWS_SECRET_ACCESS_KEY_FILE,file"`
	AwsIamRoleEndpoint            string
	AwsPartSize                   int64
	BackupCompression             CompressionType `envDefault:"gz"`
	BackupSources                 string          `envDefault:"/backup"`
	BackupFilename                string          `envDefault:"backup-%Y-%m-%dT%H-%M-%S.{{ .Extension }}"`
	BackupFilenameExpand          bool
	BackupLatestSymlink           string
	BackupArchive                 string        `envDefault:"/archive"`
	BackupRetentionDays           int32         `envDefault:"-1"`
	BackupPruningLeeway           time.Duration `envDefault:"1m"`
	BackupPruningPrefix           string
	BackupStopContainerLabel      string `envDefault:"true"`
	BackupFromSnapshot            bool
	BackupExcludeRegexp           RegexpDecoder
	BackupSkipBackendsFromPrune   []string
	GpgPassphrase                 string   `env:"GPG_PASSPHRASE"`
	GpgPassphraseFile             string   `env:"GPG_PASSPHRASE_FILE,file"`
	NotificationURLs              []string `env:"NOTIFICATION_URLS"`
	NotificationLevel             string   `envDefault:"error"`
	EmailNotificationRecipient    string
	EmailNotificationSender       string `envDefault:"noreply@nohost"`
	EmailSMTPHost                 string `env:"EMAIL_SMTP_HOST"`
	EmailSMTPPort                 int    `env:"EMAIL_SMTP_PORT" envDefault:"587"`
	EmailSMTPUsername             string `env:"EMAIL_SMTP_USERNAME"`
	EmailSMTPPassword             string `env:"EMAIL_SMTP_PASSWORD"`
	EmailSMTPPasswordFile         string `env:"EMAIL_SMTP_PASSWORD_FILE,file"`
	WebdavUrl                     string
	WebdavUrlInsecure             bool
	WebdavPath                    string `envDefault:"/"`
	WebdavUsername                string
	WebdavPassword                string `env:"WEBDAV_PASSWORD"`
	WebdavPasswordFile            string `env:"WEBDAV_PASSWORD_FILE,file"`
	SSHHostName                   string `env:"SSH_HOST_NAME"`
	SSHPort                       string `env:"SSH_PORT" envDefault:"22"`
	SSHUser                       string `env:"SSH_USER"`
	SSHPassword                   string `env:"SSH_PASSWORD"`
	SSHPasswordFile               string `env:"SSH_PASSWORD_FILE,file"`
	SSHIdentityFile               string `env:"SSH_IDENTITY_FILE" envDefault:"/root/.ssh/id_rsa"`
	SSHIdentityPassphrase         string `env:"SSH_IDENTITY_PASSPHRASE"`
	SSHIdentityPassphraseFile     string `env:"SSH_IDENTITY_PASSPHRASE_FILE,file"`
	SSHRemotePath                 string `env:"SSH_REMOTE_PATH"`
	ExecLabel                     string
	ExecForwardOutput             bool
	LockTimeout                   time.Duration `envDefault:"60m"`
	AzureStorageAccountName       string
	AzureStoragePrimaryAccountKey string
	AzureStorageContainerName     string
	AzureStoragePath              string
	AzureStorageEndpoint          string `envDefault:"https://{{ .AccountName }}.blob.core.windows.net/"`
	DropboxEndpoint               string `envDefault:"https://api.dropbox.com/"`
	DropboxOAuth2Endpoint         string `env:"DROPBOX_OAUTH2_ENDPOINT" envDefault:"https://api.dropbox.com/"`
	DropboxRefreshToken           string `env:"DROPBOX_REFRESH_TOKEN"`
	DropboxRefreshTokenFile       string `env:"DROPBOX_REFRESH_TOKEN_FILE,file"`
	DropboxAppKey                 string `env:"DROPBOX_APP_KEY"`
	DropboxAppKeyFile             string `env:"DROPBOX_APP_KEY_FILE,file"`
	DropboxAppSecret              string `env:"DROPBOX_APP_SECRET"`
	DropboxAppSecretFile          string `env:"DROPBOX_APP_SECRET_FILE,file"`
	DropboxRemotePath             string
	DropboxConcurrencyLevel       NaturalNumber `envDefault:"6"`
}

func (c *Config) getSecret(preferred string, fallback string) string {
	if preferred != "" {
		return preferred
	}
	if fallback != "" {
		return fallback
	}
	return ""
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
