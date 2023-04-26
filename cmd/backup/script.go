// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"text/template"
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/offen/docker-volume-backup/internal/storage/azure"
	"github.com/offen/docker-volume-backup/internal/storage/local"
	"github.com/offen/docker-volume-backup/internal/storage/s3"
	"github.com/offen/docker-volume-backup/internal/storage/ssh"
	"github.com/offen/docker-volume-backup/internal/storage/webdav"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/kelseyhightower/envconfig"
	"github.com/leekchan/timeutil"
	"github.com/otiai10/copy"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/sync/errgroup"
)

// script holds all the stateful information required to orchestrate a
// single backup run.
type script struct {
	cli       *client.Client
	storages  []storage.Backend
	logger    *logrus.Logger
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
func newScript() (*script, error) {
	stdOut, logBuffer := buffer(os.Stdout)
	s := &script{
		c: &Config{},
		logger: &logrus.Logger{
			Out:       stdOut,
			Formatter: new(logrus.TextFormatter),
			Hooks:     make(logrus.LevelHooks),
			Level:     logrus.InfoLevel,
		},
		stats: &Stats{
			StartTime: time.Now(),
			LogOutput: logBuffer,
			Storages: map[string]StorageStats{
				"S3":     {},
				"WebDAV": {},
				"SSH":    {},
				"Local":  {},
				"Azure":  {},
			},
		},
	}

	s.registerHook(hookLevelPlumbing, func(error) error {
		s.stats.EndTime = time.Now()
		s.stats.TookTime = s.stats.EndTime.Sub(s.stats.StartTime)
		return nil
	})

	if err := envconfig.Process("", s.c); err != nil {
		return nil, fmt.Errorf("newScript: failed to process configuration values: %w", err)
	}

	s.file = path.Join("/tmp", s.c.BackupFilename)
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
			return nil, fmt.Errorf("newScript: failed to create docker client")
		}
		s.cli = cli
	}

	logFunc := func(logType storage.LogLevel, context string, msg string, params ...any) {
		switch logType {
		case storage.LogLevelWarning:
			s.logger.Warnf("["+context+"] "+msg, params...)
		case storage.LogLevelError:
			s.logger.Errorf("["+context+"] "+msg, params...)
		case storage.LogLevelInfo:
		default:
			s.logger.Infof("["+context+"] "+msg, params...)
		}
	}

	if s.c.AwsS3BucketName != "" {
		accessKeyID, err := s.c.resolveSecret(s.c.AwsAccessKeyID, s.c.AwsAccessKeyIDFile)
		if err != nil {
			return nil, fmt.Errorf("newScript: error resolving AwsAccessKeyID: %w", err)
		}
		secretAccessKey, err := s.c.resolveSecret(s.c.AwsSecretAccessKey, s.c.AwsSecretAccessKeyFile)
		if err != nil {
			return nil, fmt.Errorf("newScript: error resolving AwsSecretAccessKey: %w", err)
		}
		s3Config := s3.Config{
			Endpoint:         s.c.AwsEndpoint,
			AccessKeyID:      accessKeyID,
			SecretAccessKey:  secretAccessKey,
			IamRoleEndpoint:  s.c.AwsIamRoleEndpoint,
			EndpointProto:    s.c.AwsEndpointProto,
			EndpointInsecure: s.c.AwsEndpointInsecure,
			RemotePath:       s.c.AwsS3Path,
			BucketName:       s.c.AwsS3BucketName,
			StorageClass:     s.c.AwsStorageClass,
			CACert:           s.c.AwsEndpointCACert.Cert,
		}
		if s3Backend, err := s3.NewStorageBackend(s3Config, logFunc); err != nil {
			return nil, err
		} else {
			s.storages = append(s.storages, s3Backend)
		}
	}

	if s.c.WebdavUrl != "" {
		webDavConfig := webdav.Config{
			URL:         s.c.WebdavUrl,
			URLInsecure: s.c.WebdavUrlInsecure,
			Username:    s.c.WebdavUsername,
			Password:    s.c.WebdavPassword,
			RemotePath:  s.c.WebdavPath,
		}
		if webdavBackend, err := webdav.NewStorageBackend(webDavConfig, logFunc); err != nil {
			return nil, err
		} else {
			s.storages = append(s.storages, webdavBackend)
		}
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
		if sshBackend, err := ssh.NewStorageBackend(sshConfig, logFunc); err != nil {
			return nil, err
		} else {
			s.storages = append(s.storages, sshBackend)
		}
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
			return nil, err
		}
		s.storages = append(s.storages, azureBackend)
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
		return nil, fmt.Errorf("newScript: unknown NOTIFICATION_LEVEL %s", s.c.NotificationLevel)
	}
	s.hookLevel = hookLevel

	if len(s.c.NotificationURLs) > 0 {
		sender, senderErr := shoutrrr.CreateSender(s.c.NotificationURLs...)
		if senderErr != nil {
			return nil, fmt.Errorf("newScript: error creating sender: %w", senderErr)
		}
		s.sender = sender

		tmpl := template.New("")
		tmpl.Funcs(templateHelpers)
		tmpl, err = tmpl.Parse(defaultNotifications)
		if err != nil {
			return nil, fmt.Errorf("newScript: unable to parse default notifications templates: %w", err)
		}

		if fi, err := os.Stat("/etc/dockervolumebackup/notifications.d"); err == nil && fi.IsDir() {
			tmpl, err = tmpl.ParseGlob("/etc/dockervolumebackup/notifications.d/*.*")
			if err != nil {
				return nil, fmt.Errorf("newScript: unable to parse user defined notifications templates: %w", err)
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

	return s, nil
}

// stopContainers stops all Docker containers that are marked as to being
// stopped during the backup and returns a function that can be called to
// restart everything that has been stopped.
func (s *script) stopContainers() (func() error, error) {
	if s.cli == nil {
		return noop, nil
	}

	allContainers, err := s.cli.ContainerList(context.Background(), types.ContainerListOptions{
		Quiet: true,
	})
	if err != nil {
		return noop, fmt.Errorf("stopContainers: error querying for containers: %w", err)
	}

	containerLabel := fmt.Sprintf(
		"docker-volume-backup.stop-during-backup=%s",
		s.c.BackupStopContainerLabel,
	)
	containersToStop, err := s.cli.ContainerList(context.Background(), types.ContainerListOptions{
		Quiet: true,
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: containerLabel,
		}),
	})

	if err != nil {
		return noop, fmt.Errorf("stopContainers: error querying for containers to stop: %w", err)
	}

	if len(containersToStop) == 0 {
		return noop, nil
	}

	s.logger.Infof(
		"Stopping %d container(s) labeled `%s` out of %d running container(s).",
		len(containersToStop),
		containerLabel,
		len(allContainers),
	)

	var stoppedContainers []types.Container
	var stopErrors []error
	for _, container := range containersToStop {
		if err := s.cli.ContainerStop(context.Background(), container.ID, nil); err != nil {
			stopErrors = append(stopErrors, err)
		} else {
			stoppedContainers = append(stoppedContainers, container)
		}
	}

	var stopError error
	if len(stopErrors) != 0 {
		stopError = fmt.Errorf(
			"stopContainers: %d error(s) stopping containers: %w",
			len(stopErrors),
			errors.Join(stopErrors...),
		)
	}

	s.stats.Containers = ContainersStats{
		All:     uint(len(allContainers)),
		ToStop:  uint(len(containersToStop)),
		Stopped: uint(len(stoppedContainers)),
	}

	return func() error {
		servicesRequiringUpdate := map[string]struct{}{}

		var restartErrors []error
		for _, container := range stoppedContainers {
			if swarmServiceName, ok := container.Labels["com.docker.swarm.service.name"]; ok {
				servicesRequiringUpdate[swarmServiceName] = struct{}{}
				continue
			}
			if err := s.cli.ContainerStart(context.Background(), container.ID, types.ContainerStartOptions{}); err != nil {
				restartErrors = append(restartErrors, err)
			}
		}

		if len(servicesRequiringUpdate) != 0 {
			services, _ := s.cli.ServiceList(context.Background(), types.ServiceListOptions{})
			for serviceName := range servicesRequiringUpdate {
				var serviceMatch swarm.Service
				for _, service := range services {
					if service.Spec.Name == serviceName {
						serviceMatch = service
						break
					}
				}
				if serviceMatch.ID == "" {
					return fmt.Errorf("stopContainers: couldn't find service with name %s", serviceName)
				}
				serviceMatch.Spec.TaskTemplate.ForceUpdate += 1
				if _, err := s.cli.ServiceUpdate(
					context.Background(), serviceMatch.ID,
					serviceMatch.Version, serviceMatch.Spec, types.ServiceUpdateOptions{},
				); err != nil {
					restartErrors = append(restartErrors, err)
				}
			}
		}

		if len(restartErrors) != 0 {
			return fmt.Errorf(
				"stopContainers: %d error(s) restarting containers and services: %w",
				len(restartErrors),
				errors.Join(restartErrors...),
			)
		}
		s.logger.Infof(
			"Restarted %d container(s) and the matching service(s).",
			len(stoppedContainers),
		)
		return nil
	}, stopError
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
			"Please use `archive-pre` and `archive-post` commands to prepare your backup sources. Refer to the README for an upgrade guide.",
		)
		backupSources = filepath.Join("/tmp", s.c.BackupSources)
		// copy before compressing guard against a situation where backup folder's content are still growing.
		s.registerHook(hookLevelPlumbing, func(error) error {
			if err := remove(backupSources); err != nil {
				return fmt.Errorf("createArchive: error removing snapshot: %w", err)
			}
			s.logger.Infof("Removed snapshot `%s`.", backupSources)
			return nil
		})
		if err := copy.Copy(s.c.BackupSources, backupSources, copy.Options{
			PreserveTimes: true,
			PreserveOwner: true,
		}); err != nil {
			return fmt.Errorf("createArchive: error creating snapshot: %w", err)
		}
		s.logger.Infof("Created snapshot of `%s` at `%s`.", s.c.BackupSources, backupSources)
	}

	tarFile := s.file
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(tarFile); err != nil {
			return fmt.Errorf("createArchive: error removing tar file: %w", err)
		}
		s.logger.Infof("Removed tar file `%s`.", tarFile)
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

	if err := createArchive(filesEligibleForBackup, backupSources, tarFile); err != nil {
		return fmt.Errorf("createArchive: error compressing backup folder: %w", err)
	}

	s.logger.Infof("Created backup of `%s` at `%s`.", backupSources, tarFile)
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
		s.logger.Infof("Removed GPG file `%s`.", gpgFile)
		return nil
	})

	outFile, err := os.Create(gpgFile)
	if err != nil {
		return fmt.Errorf("encryptArchive: error opening out file: %w", err)
	}
	defer outFile.Close()

	_, name := path.Split(s.file)
	dst, err := openpgp.SymmetricallyEncrypt(outFile, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
		IsBinary: true,
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
	s.logger.Infof("Encrypted backup using given passphrase, saving as `%s`.", s.file)
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

// must exits the script run prematurely in case the given error
// is non-nil.
func (s *script) must(err error) {
	if err != nil {
		s.logger.Errorf("Fatal error running backup: %s", err)
		panic(err)
	}
}
