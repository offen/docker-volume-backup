// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path"
	"text/template"
	"time"

	"github.com/offen/docker-volume-backup/internal/errwrap"
	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/offen/docker-volume-backup/internal/storage/azure"
	"github.com/offen/docker-volume-backup/internal/storage/dropbox"
	"github.com/offen/docker-volume-backup/internal/storage/googledrive"
	"github.com/offen/docker-volume-backup/internal/storage/local"
	"github.com/offen/docker-volume-backup/internal/storage/s3"
	"github.com/offen/docker-volume-backup/internal/storage/ssh"
	"github.com/offen/docker-volume-backup/internal/storage/webdav"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/docker/docker/client"
	"github.com/leekchan/timeutil"
)

// script holds all the stateful information required to orchestrate a
// single backup run.
type script struct {
	cli       *client.Client
	storages  []storage.Backend
	logger    *slog.Logger
	sender    *router.ServiceRouter
	template  *template.Template
	hooks     []hook
	hookLevel hookLevel

	file  string
	stats *Stats

	encounteredLock bool

	c *Config
}

// newScript creates all resources needed for the script to perform actions against
// remote resources like the Docker engine or remote storage locations. All
// reading from env vars or other configuration sources is expected to happen
// in this method.
func newScript(c *Config) *script {
	stdOut, logBuffer := buffer(os.Stdout)
	return &script{
		c:      c,
		logger: slog.New(slog.NewTextHandler(stdOut, nil)),
		stats: &Stats{
			StartTime: time.Now(),
			LogOutput: logBuffer,
			Storages: map[string]StorageStats{
				"S3":          {},
				"WebDAV":      {},
				"SSH":         {},
				"Local":       {},
				"Azure":       {},
				"Dropbox":     {},
				"GoogleDrive": {},
			},
		},
	}
}

func (s *script) init() error {
	s.registerHook(hookLevelPlumbing, func(error) error {
		s.stats.EndTime = time.Now()
		s.stats.TookTime = s.stats.EndTime.Sub(s.stats.StartTime)
		return nil
	})

	s.file = path.Join("/tmp", s.c.BackupFilename)

	tmplFileName, tErr := template.New("extension").Parse(s.file)
	if tErr != nil {
		return errwrap.Wrap(tErr, "unable to parse backup file extension template")
	}

	var bf bytes.Buffer
	if tErr := tmplFileName.Execute(&bf, map[string]string{
		"Extension": func() string {
			if s.c.BackupCompression == "none" {
				return "tar"
			}
			return fmt.Sprintf("tar.%s", s.c.BackupCompression)
		}(),
	}); tErr != nil {
		return errwrap.Wrap(tErr, "error executing backup file extension template")
	}
	s.file = bf.String()

	if s.c.BackupFilenameExpand {
		s.file = os.ExpandEnv(s.file)
		s.c.BackupLatestSymlink = os.ExpandEnv(s.c.BackupLatestSymlink)
		s.c.BackupPruningPrefix = os.ExpandEnv(s.c.BackupPruningPrefix)
	}
	s.file = timeutil.Strftime(&s.stats.StartTime, s.file)

	_, err := os.Stat("/var/run/docker.sock")
	_, dockerHostSet := os.LookupEnv("DOCKER_HOST")
	if !os.IsNotExist(err) || dockerHostSet {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return errwrap.Wrap(err, "failed to create docker client")
		}
		s.cli = cli
		s.registerHook(hookLevelPlumbing, func(err error) error {
			if err := s.cli.Close(); err != nil {
				return errwrap.Wrap(err, "failed to close docker client")
			}
			return nil
		})
	}

	logFunc := func(logType storage.LogLevel, context string, msg string, params ...any) {
		switch logType {
		case storage.LogLevelWarning:
			s.logger.Warn(fmt.Sprintf(msg, params...), "storage", context)
		default:
			s.logger.Info(fmt.Sprintf(msg, params...), "storage", context)
		}
	}

	if s.c.AwsS3BucketName != "" {
		s3Config := s3.Config{
			Endpoint:         s.c.AwsEndpoint,
			AccessKeyID:      s.c.AwsAccessKeyID,
			SecretAccessKey:  s.c.AwsSecretAccessKey,
			IamRoleEndpoint:  s.c.AwsIamRoleEndpoint,
			EndpointProto:    s.c.AwsEndpointProto,
			EndpointInsecure: s.c.AwsEndpointInsecure,
			RemotePath:       s.c.AwsS3Path,
			BucketName:       s.c.AwsS3BucketName,
			StorageClass:     s.c.AwsStorageClass,
			CACert:           s.c.AwsEndpointCACert.Cert,
			PartSize:         s.c.AwsPartSize,
		}
		s3Backend, err := s3.NewStorageBackend(s3Config, logFunc)
		if err != nil {
			return errwrap.Wrap(err, "error creating s3 storage backend")
		}
		s.storages = append(s.storages, s3Backend)
	}

	if s.c.WebdavUrl != "" {
		webDavConfig := webdav.Config{
			URL:         s.c.WebdavUrl,
			URLInsecure: s.c.WebdavUrlInsecure,
			Username:    s.c.WebdavUsername,
			Password:    s.c.WebdavPassword,
			RemotePath:  s.c.WebdavPath,
		}
		webdavBackend, err := webdav.NewStorageBackend(webDavConfig, logFunc)
		if err != nil {
			return errwrap.Wrap(err, "error creating webdav storage backend")
		}
		s.storages = append(s.storages, webdavBackend)
	}

	if s.c.SSHHostName != "" {
		sshConfig := ssh.Config{
			HostName:           s.c.SSHHostName,
			Port:               s.c.SSHPort,
			User:               s.c.SSHUser,
			Password:           s.c.SSHPassword,
			IdentityFile:       s.c.SSHIdentityFile,
			IdentityPassphrase: s.c.SSHIdentityPassphrase,
			RemotePath:         s.c.SSHRemotePath,
		}
		sshBackend, err := ssh.NewStorageBackend(sshConfig, logFunc)
		if err != nil {
			return errwrap.Wrap(err, "error creating ssh storage backend")
		}
		s.storages = append(s.storages, sshBackend)
	}

	if _, err := os.Stat(s.c.BackupArchive); !os.IsNotExist(err) {
		localConfig := local.Config{
			ArchivePath:   s.c.BackupArchive,
			LatestSymlink: s.c.BackupLatestSymlink,
		}
		localBackend := local.NewStorageBackend(localConfig, logFunc)
		s.storages = append(s.storages, localBackend)
	}

	if s.c.AzureStorageAccountName != "" {
		azureConfig := azure.Config{
			ContainerName:     s.c.AzureStorageContainerName,
			AccountName:       s.c.AzureStorageAccountName,
			PrimaryAccountKey: s.c.AzureStoragePrimaryAccountKey,
			Endpoint:          s.c.AzureStorageEndpoint,
			RemotePath:        s.c.AzureStoragePath,
			ConnectionString:  s.c.AzureStorageConnectionString,
			AccessTier:        s.c.AzureStorageAccessTier,
		}
		azureBackend, err := azure.NewStorageBackend(azureConfig, logFunc)
		if err != nil {
			return errwrap.Wrap(err, "error creating azure storage backend")
		}
		s.storages = append(s.storages, azureBackend)
	}

	if s.c.DropboxRefreshToken != "" && s.c.DropboxAppKey != "" && s.c.DropboxAppSecret != "" {
		dropboxConfig := dropbox.Config{
			Endpoint:         s.c.DropboxEndpoint,
			OAuth2Endpoint:   s.c.DropboxOAuth2Endpoint,
			RefreshToken:     s.c.DropboxRefreshToken,
			AppKey:           s.c.DropboxAppKey,
			AppSecret:        s.c.DropboxAppSecret,
			RemotePath:       s.c.DropboxRemotePath,
			ConcurrencyLevel: s.c.DropboxConcurrencyLevel.Int(),
		}
		dropboxBackend, err := dropbox.NewStorageBackend(dropboxConfig, logFunc)
		if err != nil {
			return errwrap.Wrap(err, "error creating dropbox storage backend")
		}
		s.storages = append(s.storages, dropboxBackend)
	}

	if s.c.GoogleDriveCredentialsJSON != "" {
		googleDriveConfig := googledrive.Config{
			CredentialsJSON:    s.c.GoogleDriveCredentialsJSON,
			FolderID:           s.c.GoogleDriveFolderID,
			ImpersonateSubject: s.c.GoogleDriveImpersonateSubject,
			Endpoint:           s.c.GoogleDriveEndpoint,
			TokenURL:           s.c.GoogleDriveTokenURL,
		}
		googleDriveBackend, err := googledrive.NewStorageBackend(googleDriveConfig, logFunc)
		if err != nil {
			return errwrap.Wrap(err, "error creating googledrive storage backend")
		}
		s.storages = append(s.storages, googleDriveBackend)
	}

	if s.c.EmailNotificationRecipient != "" {
		emailURL := fmt.Sprintf(
			"smtp://%s:%s@%s:%d/?from=%s&to=%s",
			s.c.EmailSMTPUsername,
			s.c.EmailSMTPPassword,
			s.c.EmailSMTPHost,
			s.c.EmailSMTPPort,
			s.c.EmailNotificationSender,
			s.c.EmailNotificationRecipient,
		)
		s.c.NotificationURLs = append(s.c.NotificationURLs, emailURL)
		s.logger.Warn(
			"Using EMAIL_* keys for providing notification configuration has been deprecated and will be removed in the next major version.",
		)
		s.logger.Warn(
			"Please use NOTIFICATION_URLS instead. Refer to the README for an upgrade guide.",
		)
	}

	hookLevel, ok := hookLevels[s.c.NotificationLevel]
	if !ok {
		return errwrap.Wrap(nil, fmt.Sprintf("unknown NOTIFICATION_LEVEL %s", s.c.NotificationLevel))
	}
	s.hookLevel = hookLevel

	if len(s.c.NotificationURLs) > 0 {
		sender, senderErr := shoutrrr.CreateSender(s.c.NotificationURLs...)
		if senderErr != nil {
			return errwrap.Wrap(senderErr, "error creating sender")
		}
		s.sender = sender

		tmpl := template.New("")
		tmpl.Funcs(templateHelpers)
		tmpl, err = tmpl.Parse(defaultNotifications)
		if err != nil {
			return errwrap.Wrap(err, "unable to parse default notifications templates")
		}

		if fi, err := os.Stat("/etc/dockervolumebackup/notifications.d"); err == nil && fi.IsDir() {
			tmpl, err = tmpl.ParseGlob("/etc/dockervolumebackup/notifications.d/*.*")
			if err != nil {
				return errwrap.Wrap(err, "unable to parse user defined notifications templates")
			}
		}
		s.template = tmpl

		// To prevent duplicate notifications, ensure the regsistered callbacks
		// run mutually exclusive.
		s.registerHook(hookLevelError, func(err error) error {
			if err == nil {
				return nil
			}
			return s.notifyFailure(err)
		})
		s.registerHook(hookLevelInfo, func(err error) error {
			if err != nil {
				return nil
			}
			return s.notifySuccess()
		})
	}

	return nil
}
