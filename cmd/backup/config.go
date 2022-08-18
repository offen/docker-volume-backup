// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"regexp"
	"time"
)

// Config holds all configuration values that are expected to be set
// by users.
type Config struct {
	AwsS3BucketName            string        `split_words:"true"`
	AwsS3Path                  string        `split_words:"true"`
	AwsEndpoint                string        `split_words:"true" default:"s3.amazonaws.com"`
	AwsEndpointProto           string        `split_words:"true" default:"https"`
	AwsEndpointInsecure        bool          `split_words:"true"`
	AwsStorageClass            string        `split_words:"true"`
	AwsAccessKeyID             string        `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey         string        `split_words:"true"`
	AwsIamRoleEndpoint         string        `split_words:"true"`
	BackupSources              string        `split_words:"true" default:"/backup"`
	BackupFilename             string        `split_words:"true" default:"backup-%Y-%m-%dT%H-%M-%S.tar.gz"`
	BackupFilenameExpand       bool          `split_words:"true"`
	BackupLatestSymlink        string        `split_words:"true"`
	BackupArchive              string        `split_words:"true" default:"/archive"`
	BackupRetentionDays        int32         `split_words:"true" default:"-1"`
	BackupPruningLeeway        time.Duration `split_words:"true" default:"1m"`
	BackupPruningPrefix        string        `split_words:"true"`
	BackupStopContainerLabel   string        `split_words:"true" default:"true"`
	BackupFromSnapshot         bool          `split_words:"true"`
	BackupExcludeRegexp        RegexpDecoder `split_words:"true"`
	GpgPassphrase              string        `split_words:"true"`
	NotificationURLs           []string      `envconfig:"NOTIFICATION_URLS"`
	NotificationLevel          string        `split_words:"true" default:"error"`
	EmailNotificationRecipient string        `split_words:"true"`
	EmailNotificationSender    string        `split_words:"true" default:"noreply@nohost"`
	EmailSMTPHost              string        `envconfig:"EMAIL_SMTP_HOST"`
	EmailSMTPPort              int           `envconfig:"EMAIL_SMTP_PORT" default:"587"`
	EmailSMTPUsername          string        `envconfig:"EMAIL_SMTP_USERNAME"`
	EmailSMTPPassword          string        `envconfig:"EMAIL_SMTP_PASSWORD"`
	WebdavUrl                  string        `split_words:"true"`
	WebdavUrlInsecure          bool          `split_words:"true"`
	WebdavPath                 string        `split_words:"true" default:"/"`
	WebdavUsername             string        `split_words:"true"`
	WebdavPassword             string        `split_words:"true"`
	SSHHostName                string        `split_words:"true"`
	SSHPort                    string        `split_words:"true" default:"22"`
	SSHUser                    string        `split_words:"true"`
	SSHPassword                string        `split_words:"true"`
	SSHIdentityFile            string        `split_words:"true" default:"/root/.ssh/id_rsa"`
	SSHIdentityPassphrase      string        `split_words:"true"`
	SSHRemotePath              string        `split_words:"true"`
	ExecLabel                  string        `split_words:"true"`
	ExecForwardOutput          bool          `split_words:"true"`
	LockTimeout                time.Duration `split_words:"true" default:"60m"`
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
		return fmt.Errorf("config: error compiling given regexp `%s`: %w", v, err)
	}
	*r = RegexpDecoder{Re: re}
	return nil
}
