// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/offen/docker-volume-backup/internal/storage/azure"
	"github.com/offen/docker-volume-backup/internal/storage/dropbox"
	"github.com/offen/docker-volume-backup/internal/storage/local"
	"github.com/offen/docker-volume-backup/internal/storage/s3"
	"github.com/offen/docker-volume-backup/internal/storage/ssh"
	"github.com/offen/docker-volume-backup/internal/storage/webdav"

	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/docker/docker/client"
	"github.com/leekchan/timeutil"
	"github.com/otiai10/copy"
	"golang.org/x/sync/errgroup"
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
func newScript(c *Config, envVars map[string]string) (*script, func() error, error) {
	stdOut, logBuffer := buffer(os.Stdout)
	s := &script{
		c:      c,
		logger: slog.New(slog.NewTextHandler(stdOut, nil)),
		stats: &Stats{
			StartTime: time.Now(),
			LogOutput: logBuffer,
			Storages: map[string]StorageStats{
				"S3":      {},
				"WebDAV":  {},
				"SSH":     {},
				"Local":   {},
				"Azure":   {},
				"Dropbox": {},
			},
		},
	}

	unlock, err := s.lock("/var/lock/dockervolumebackup.lock")
	if err != nil {
		return nil, noop, fmt.Errorf("runScript: error acquiring file lock: %w", err)
	}

	for key, value := range envVars {
		currentVal, currentOk := os.LookupEnv(key)
		defer func(currentKey, currentVal string, currentOk bool) {
			if !currentOk {
				_ = os.Unsetenv(currentKey)
			} else {
				_ = os.Setenv(currentKey, currentVal)
			}
		}(key, currentVal, currentOk)

		if err := os.Setenv(key, value); err != nil {
			return nil, unlock, fmt.Errorf(
				"Unexpected error overloading environment %s: %w",
				s.c.BackupCronExpression,
				err,
			)
		}
	}
	s.registerHook(hookLevelPlumbing, func(error) error {
		s.stats.EndTime = time.Now()
		s.stats.TookTime = s.stats.EndTime.Sub(s.stats.StartTime)
		return nil
	})

	s.file = path.Join("/tmp", s.c.BackupFilename)

	tmplFileName, tErr := template.New("extension").Parse(s.file)
	if tErr != nil {
		return nil, unlock, fmt.Errorf("newScript: unable to parse backup file extension template: %w", tErr)
	}

	var bf bytes.Buffer
	if tErr := tmplFileName.Execute(&bf, map[string]string{
		"Extension": fmt.Sprintf("tar.%s", s.c.BackupCompression),
	}); tErr != nil {
		return nil, unlock, fmt.Errorf("newScript: error executing backup file extension template: %w", tErr)
	}
	s.file = bf.String()

	if s.c.BackupFilenameExpand {
		s.file = os.ExpandEnv(s.file)
		s.c.BackupLatestSymlink = os.ExpandEnv(s.c.BackupLatestSymlink)
		s.c.BackupPruningPrefix = os.ExpandEnv(s.c.BackupPruningPrefix)
	}
	s.file = timeutil.Strftime(&s.stats.StartTime, s.file)

	_, err = os.Stat("/var/run/docker.sock")
	_, dockerHostSet := os.LookupEnv("DOCKER_HOST")
	if !os.IsNotExist(err) || dockerHostSet {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, unlock, fmt.Errorf("newScript: failed to create docker client")
		}
		s.cli = cli
		s.registerHook(hookLevelPlumbing, func(err error) error {
			if err := s.cli.Close(); err != nil {
				return fmt.Errorf("newScript: failed to close docker client: %w", err)
			}
			return nil
		})
	}

	logFunc := func(logType storage.LogLevel, context string, msg string, params ...any) {
		switch logType {
		case storage.LogLevelWarning:
			s.logger.Warn(fmt.Sprintf(msg, params...), "storage", context)
		case storage.LogLevelError:
			s.logger.Error(fmt.Sprintf(msg, params...), "storage", context)
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
			return nil, unlock, fmt.Errorf("newScript: error creating s3 storage backend: %w", err)
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
			return nil, unlock, fmt.Errorf("newScript: error creating webdav storage backend: %w", err)
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
			return nil, unlock, fmt.Errorf("newScript: error creating ssh storage backend: %w", err)
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
		}
		azureBackend, err := azure.NewStorageBackend(azureConfig, logFunc)
		if err != nil {
			return nil, unlock, fmt.Errorf("newScript: error creating azure storage backend: %w", err)
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
			return nil, unlock, fmt.Errorf("newScript: error creating dropbox storage backend: %w", err)
		}
		s.storages = append(s.storages, dropboxBackend)
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
		return nil, unlock, fmt.Errorf("newScript: unknown NOTIFICATION_LEVEL %s", s.c.NotificationLevel)
	}
	s.hookLevel = hookLevel

	if len(s.c.NotificationURLs) > 0 {
		sender, senderErr := shoutrrr.CreateSender(s.c.NotificationURLs...)
		if senderErr != nil {
			return nil, unlock, fmt.Errorf("newScript: error creating sender: %w", senderErr)
		}
		s.sender = sender

		tmpl := template.New("")
		tmpl.Funcs(templateHelpers)
		tmpl, err = tmpl.Parse(defaultNotifications)
		if err != nil {
			return nil, unlock, fmt.Errorf("newScript: unable to parse default notifications templates: %w", err)
		}

		if fi, err := os.Stat("/etc/dockervolumebackup/notifications.d"); err == nil && fi.IsDir() {
			tmpl, err = tmpl.ParseGlob("/etc/dockervolumebackup/notifications.d/*.*")
			if err != nil {
				return nil, unlock, fmt.Errorf("newScript: unable to parse user defined notifications templates: %w", err)
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

	return s, unlock, nil
}

// createArchive creates a tar archive of the configured backup location and
// saves it to disk.
func (s *script) createArchive() error {
	backupSources := s.c.BackupSources

	if s.c.BackupFromSnapshot {
		s.logger.Warn(
			"Using BACKUP_FROM_SNAPSHOT has been deprecated and will be removed in the next major version.",
		)
		s.logger.Warn(
			"Please use `archive-pre` and `archive-post` commands to prepare your backup sources. Refer to the documentation for an upgrade guide.",
		)
		backupSources = filepath.Join("/tmp", s.c.BackupSources)
		// copy before compressing guard against a situation where backup folder's content are still growing.
		s.registerHook(hookLevelPlumbing, func(error) error {
			if err := remove(backupSources); err != nil {
				return fmt.Errorf("createArchive: error removing snapshot: %w", err)
			}
			s.logger.Info(
				fmt.Sprintf("Removed snapshot `%s`.", backupSources),
			)
			return nil
		})
		if err := copy.Copy(s.c.BackupSources, backupSources, copy.Options{
			PreserveTimes: true,
			PreserveOwner: true,
		}); err != nil {
			return fmt.Errorf("createArchive: error creating snapshot: %w", err)
		}
		s.logger.Info(
			fmt.Sprintf("Created snapshot of `%s` at `%s`.", s.c.BackupSources, backupSources),
		)
	}

	tarFile := s.file
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(tarFile); err != nil {
			return fmt.Errorf("createArchive: error removing tar file: %w", err)
		}
		s.logger.Info(
			fmt.Sprintf("Removed tar file `%s`.", tarFile),
		)
		return nil
	})

	backupPath, err := filepath.Abs(stripTrailingSlashes(backupSources))
	if err != nil {
		return fmt.Errorf("createArchive: error getting absolute path: %w", err)
	}

	var filesEligibleForBackup []string
	if err := filepath.WalkDir(backupPath, func(path string, di fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if s.c.BackupExcludeRegexp.Re != nil && s.c.BackupExcludeRegexp.Re.MatchString(path) {
			return nil
		}
		filesEligibleForBackup = append(filesEligibleForBackup, path)
		return nil
	}); err != nil {
		return fmt.Errorf("createArchive: error walking filesystem tree: %w", err)
	}

	if err := createArchive(filesEligibleForBackup, backupSources, tarFile, s.c.BackupCompression.String(), s.c.GzipParallelism.Int()); err != nil {
		return fmt.Errorf("createArchive: error compressing backup folder: %w", err)
	}

	s.logger.Info(
		fmt.Sprintf("Created backup of `%s` at `%s`.", backupSources, tarFile),
	)
	return nil
}

// encryptArchive encrypts the backup file using PGP and the configured passphrase.
// In case no passphrase is given it returns early, leaving the backup file
// untouched.
func (s *script) encryptArchive() error {
	if s.c.GpgPassphrase == "" {
		return nil
	}

	gpgFile := fmt.Sprintf("%s.gpg", s.file)
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(gpgFile); err != nil {
			return fmt.Errorf("encryptArchive: error removing gpg file: %w", err)
		}
		s.logger.Info(
			fmt.Sprintf("Removed GPG file `%s`.", gpgFile),
		)
		return nil
	})

	outFile, err := os.Create(gpgFile)
	if err != nil {
		return fmt.Errorf("encryptArchive: error opening out file: %w", err)
	}
	defer outFile.Close()

	_, name := path.Split(s.file)
	dst, err := openpgp.SymmetricallyEncrypt(outFile, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
		FileName: name,
	}, nil)
	if err != nil {
		return fmt.Errorf("encryptArchive: error encrypting backup file: %w", err)
	}
	defer dst.Close()

	src, err := os.Open(s.file)
	if err != nil {
		return fmt.Errorf("encryptArchive: error opening backup file `%s`: %w", s.file, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("encryptArchive: error writing ciphertext to file: %w", err)
	}

	s.file = gpgFile
	s.logger.Info(
		fmt.Sprintf("Encrypted backup using given passphrase, saving as `%s`.", s.file),
	)
	return nil
}

// copyArchive makes sure the backup file is copied to both local and remote locations
// as per the given configuration.
func (s *script) copyArchive() error {
	_, name := path.Split(s.file)
	if stat, err := os.Stat(s.file); err != nil {
		return fmt.Errorf("copyArchive: unable to stat backup file: %w", err)
	} else {
		size := stat.Size()
		s.stats.BackupFile = BackupFileStats{
			Size:     uint64(size),
			Name:     name,
			FullPath: s.file,
		}
	}

	eg := errgroup.Group{}
	for _, backend := range s.storages {
		b := backend
		eg.Go(func() error {
			return b.Copy(s.file)
		})
	}
	if err := eg.Wait(); err != nil {
		return fmt.Errorf("copyArchive: error copying archive: %w", err)
	}

	return nil
}

// pruneBackups rotates away backups from local and remote storages using
// the given configuration. In case the given configuration would delete all
// backups, it does nothing instead and logs a warning.
func (s *script) pruneBackups() error {
	if s.c.BackupRetentionDays < 0 {
		return nil
	}

	deadline := time.Now().AddDate(0, 0, -int(s.c.BackupRetentionDays)).Add(s.c.BackupPruningLeeway)

	eg := errgroup.Group{}
	for _, backend := range s.storages {
		b := backend
		eg.Go(func() error {
			if skipPrune(b.Name(), s.c.BackupSkipBackendsFromPrune) {
				s.logger.Info(
					fmt.Sprintf("Skipping pruning for backend `%s`.", b.Name()),
				)
				return nil
			}
			stats, err := b.Prune(deadline, s.c.BackupPruningPrefix)
			if err != nil {
				return err
			}
			s.stats.Lock()
			s.stats.Storages[b.Name()] = StorageStats{
				Total:  stats.Total,
				Pruned: stats.Pruned,
			}
			s.stats.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("pruneBackups: error pruning backups: %w", err)
	}

	return nil
}

// skipPrune returns true if the given backend name is contained in the
// list of skipped backends.
func skipPrune(name string, skippedBackends []string) bool {
	return slices.ContainsFunc(
		skippedBackends,
		func(b string) bool {
			return strings.EqualFold(b, name) // ignore case on both sides
		},
	)
}
