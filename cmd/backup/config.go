// Copyright 2022 - offen.software <hioffen@posteo.de>
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

	"github.com/offen/docker-volume-backup/internal/errwrap"
)

// Config holds all configuration values that are expected to be set
// by users.
type Config struct {
	AwsS3BucketName               string          `split_words:"true"`
	AwsS3Path                     string          `split_words:"true"`
	AwsEndpoint                   string          `split_words:"true" default:"s3.amazonaws.com"`
	AwsEndpointProto              string          `split_words:"true" default:"https"`
	AwsEndpointInsecure           bool            `split_words:"true"`
	AwsEndpointCACert             CertDecoder     `envconfig:"AWS_ENDPOINT_CA_CERT"`
	AwsStorageClass               string          `split_words:"true"`
	AwsAccessKeyID                string          `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey            string          `split_words:"true"`
	AwsIamRoleEndpoint            string          `split_words:"true"`
	AwsPartSize                   int64           `split_words:"true"`
	BackupCompression             CompressionType `split_words:"true" default:"gz"`
	GzipParallelism               WholeNumber     `split_words:"true" default:"1"`
	BackupSources                 string          `split_words:"true" default:"/backup"`
	BackupFilename                string          `split_words:"true" default:"backup-%Y-%m-%dT%H-%M-%S.{{ .Extension }}"`
	BackupFilenameExpand          bool            `split_words:"true"`
	BackupLatestSymlink           string          `split_words:"true"`
	BackupArchive                 string          `split_words:"true" default:"/archive"`
	BackupCronExpression          string          `split_words:"true" default:"@daily"`
	BackupRetentionDays           int32           `split_words:"true" default:"-1"`
	BackupPruningLeeway           time.Duration   `split_words:"true" default:"1m"`
	BackupPruningPrefix           string          `split_words:"true"`
	BackupStopContainerLabel      string          `split_words:"true"`
	BackupStopDuringBackupLabel   string          `split_words:"true" default:"true"`
	BackupStopServiceTimeout      time.Duration   `split_words:"true" default:"5m"`
	BackupFromSnapshot            bool            `split_words:"true"`
	BackupExcludeRegexp           RegexpDecoder   `split_words:"true"`
	BackupSkipBackendsFromPrune   []string        `split_words:"true"`
	GpgPassphrase                 string          `split_words:"true"`
	GpgPublicKeyRing              string          `split_words:"true"`
	AgePassphrase                 string          `split_words:"true"`
	AgePublicKeys                 []string        `split_words:"true"`
	NotificationURLs              []string        `envconfig:"NOTIFICATION_URLS"`
	NotificationLevel             string          `split_words:"true" default:"error"`
	EmailNotificationRecipient    string          `split_words:"true"`
	EmailNotificationSender       string          `split_words:"true" default:"noreply@nohost"`
	EmailSMTPHost                 string          `envconfig:"EMAIL_SMTP_HOST"`
	EmailSMTPPort                 int             `envconfig:"EMAIL_SMTP_PORT" default:"587"`
	EmailSMTPUsername             string          `envconfig:"EMAIL_SMTP_USERNAME"`
	EmailSMTPPassword             string          `envconfig:"EMAIL_SMTP_PASSWORD"`
	WebdavUrl                     string          `split_words:"true"`
	WebdavUrlInsecure             bool            `split_words:"true"`
	WebdavPath                    string          `split_words:"true" default:"/"`
	WebdavUsername                string          `split_words:"true"`
	WebdavPassword                string          `split_words:"true"`
	SSHHostName                   string          `split_words:"true"`
	SSHPort                       string          `split_words:"true" default:"22"`
	SSHUser                       string          `split_words:"true"`
	SSHPassword                   string          `split_words:"true"`
	SSHIdentityFile               string          `split_words:"true" default:"/root/.ssh/id_rsa"`
	SSHIdentityPassphrase         string          `split_words:"true"`
	SSHRemotePath                 string          `split_words:"true"`
	ExecLabel                     string          `split_words:"true"`
	ExecForwardOutput             bool            `split_words:"true"`
	LockTimeout                   time.Duration   `split_words:"true" default:"60m"`
	AzureStorageAccountName       string          `split_words:"true"`
	AzureStoragePrimaryAccountKey string          `split_words:"true"`
	AzureStorageConnectionString  string          `split_words:"true"`
	AzureStorageContainerName     string          `split_words:"true"`
	AzureStoragePath              string          `split_words:"true"`
	AzureStorageEndpoint          string          `split_words:"true" default:"https://{{ .AccountName }}.blob.core.windows.net/"`
	AzureStorageAccessTier        string          `split_words:"true"`
	DropboxEndpoint               string          `split_words:"true" default:"https://api.dropbox.com/"`
	DropboxOAuth2Endpoint         string          `envconfig:"DROPBOX_OAUTH2_ENDPOINT" default:"https://api.dropbox.com/"`
	DropboxRefreshToken           string          `split_words:"true"`
	DropboxAppKey                 string          `split_words:"true"`
	DropboxAppSecret              string          `split_words:"true"`
	DropboxRemotePath             string          `split_words:"true"`
	DropboxConcurrencyLevel       NaturalNumber   `split_words:"true" default:"6"`
	GoogleDriveCredentialsJSON    string          `split_words:"true"`
	GoogleDriveFolderID           string          `split_words:"true"`
	GoogleDriveImpersonateSubject string          `split_words:"true"`
	GoogleDriveEndpoint           string          `split_words:"true"`
	GoogleDriveTokenURL           string          `split_words:"true"`
	source                        string
	additionalEnvVars             map[string]string
}

type CompressionType string

func (c *CompressionType) Decode(v string) error {
	switch v {
	case "none", "gz", "zst":
		*c = CompressionType(v)
		return nil
	default:
		return errwrap.Wrap(nil, fmt.Sprintf("error decoding compression type %s", v))
	}
}

func (c *CompressionType) String() string {
	return string(*c)
}

type CertDecoder struct {
	Cert *x509.Certificate
}

func (c *CertDecoder) Decode(v string) error {
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
		return errwrap.Wrap(err, "error parsing certificate")
	}
	*c = CertDecoder{Cert: cert}
	return nil
}

type RegexpDecoder struct {
	Re *regexp.Regexp
}

func (r *RegexpDecoder) Decode(v string) error {
	if v == "" {
		return nil
	}
	re, err := regexp.Compile(v)
	if err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error compiling given regexp `%s`", v))
	}
	*r = RegexpDecoder{Re: re}
	return nil
}

// NaturalNumber is a type that can be used to decode a positive, non-zero natural number
type NaturalNumber int

func (n *NaturalNumber) Decode(v string) error {
	asInt, err := strconv.Atoi(v)
	if err != nil {
		return errwrap.Wrap(nil, fmt.Sprintf("error converting %s to int", v))
	}
	if asInt <= 0 {
		return errwrap.Wrap(nil, fmt.Sprintf("expected a natural number, got %d", asInt))
	}
	*n = NaturalNumber(asInt)
	return nil
}

func (n *NaturalNumber) Int() int {
	return int(*n)
}

// WholeNumber is a type that can be used to decode a positive whole number, including zero
type WholeNumber int

func (n *WholeNumber) Decode(v string) error {
	asInt, err := strconv.Atoi(v)
	if err != nil {
		return errwrap.Wrap(nil, fmt.Sprintf("error converting %s to int", v))
	}
	if asInt < 0 {
		return errwrap.Wrap(nil, fmt.Sprintf("expected a whole, positive number, including zero. Got %d", asInt))
	}
	*n = WholeNumber(asInt)
	return nil
}

func (n *WholeNumber) Int() int {
	return int(*n)
}

type envVarLookup struct {
	ok    bool
	key   string
	value string
}

// applyEnv sets the values in `additionalEnvVars` as environment variables.
// It returns a function that reverts all values that have been set to its
// previous state.
func (c *Config) applyEnv() (func() error, error) {
	lookups := []envVarLookup{}

	unset := func() error {
		for _, lookup := range lookups {
			if !lookup.ok {
				if err := os.Unsetenv(lookup.key); err != nil {
					return errwrap.Wrap(err, fmt.Sprintf("error unsetting env var %s", lookup.key))
				}
				continue
			}
			if err := os.Setenv(lookup.key, lookup.value); err != nil {
				return errwrap.Wrap(err, fmt.Sprintf("error setting back env var %s", lookup.key))
			}
		}
		return nil
	}

	for key, value := range c.additionalEnvVars {
		current, ok := os.LookupEnv(key)
		lookups = append(lookups, envVarLookup{ok: ok, key: key, value: current})
		if err := os.Setenv(key, value); err != nil {
			return unset, errwrap.Wrap(err, "error setting env var")
		}
	}
	return unset, nil
}
