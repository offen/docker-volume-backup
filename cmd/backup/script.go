// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	"github.com/docker/cli/cli/command/service/progress"
	"github.com/docker/docker/api/types"
	ctr "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/leekchan/timeutil"
	"github.com/offen/envconfig"
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
func newScript() (*script, error) {
	stdOut, logBuffer := buffer(os.Stdout)
	s := &script{
		c:      &Config{},
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

	s.registerHook(hookLevelPlumbing, func(error) error {
		s.stats.EndTime = time.Now()
		s.stats.TookTime = s.stats.EndTime.Sub(s.stats.StartTime)
		return nil
	})

	envconfig.Lookup = func(key string) (string, bool) {
		value, okValue := os.LookupEnv(key)
		location, okFile := os.LookupEnv(key + "_FILE")

		switch {
		case okValue && !okFile: // only value
			return value, true
		case !okValue && okFile: // only file
			contents, err := os.ReadFile(location)
			if err != nil {
				s.must(fmt.Errorf("newScript: failed to read %s! Error: %s", location, err))
				return "", false
			}
			return string(contents), true
		case okValue && okFile: // both
			s.must(fmt.Errorf("newScript: both %s and %s are set!", key, key+"_FILE"))
			return "", false
		default: // neither, ignore
			return "", false
		}
	}

	if err := envconfig.Process("", s.c); err != nil {
		return nil, fmt.Errorf("newScript: failed to process configuration values: %w", err)
	}

	s.file = path.Join("/tmp", s.c.BackupFilename)

	tmplFileName, tErr := template.New("extension").Parse(s.file)
	if tErr != nil {
		return nil, fmt.Errorf("newScript: unable to parse backup file extension template: %w", tErr)
	}

	var bf bytes.Buffer
	if tErr := tmplFileName.Execute(&bf, map[string]string{
		"Extension": fmt.Sprintf("tar.%s", s.c.BackupCompression),
	}); tErr != nil {
		return nil, fmt.Errorf("newScript: error executing backup file extension template: %w", tErr)
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
			return nil, fmt.Errorf("newScript: failed to create docker client")
		}
		s.cli = cli
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
			return nil, fmt.Errorf("newScript: error creating s3 storage backend: %w", err)
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
			return nil, fmt.Errorf("newScript: error creating webdav storage backend: %w", err)
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
			return nil, fmt.Errorf("newScript: error creating ssh storage backend: %w", err)
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
			return nil, fmt.Errorf("newScript: error creating azure storage backend: %w", err)
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
			return nil, fmt.Errorf("newScript: error creating dropbox storage backend: %w", err)
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

// stopContainersAndServices stops all Docker containers that are marked as to being
// stopped during the backup and returns a function that can be called to
// restart everything that has been stopped.
func (s *script) stopContainersAndServices() (func() error, error) {
	if s.cli == nil {
		return noop, nil
	}

	dockerInfo, err := s.cli.Info(context.Background())
	if err != nil {
		return noop, fmt.Errorf("(*script).stopContainersAndServices: error getting docker info: %w", err)
	}
	isDockerSwarm := dockerInfo.Swarm.LocalNodeState != "inactive"
	discardWriter := &noopWriteCloser{io.Discard}

	filterMatchLabel := fmt.Sprintf(
		"docker-volume-backup.stop-during-backup=%s",
		s.c.BackupStopContainerLabel,
	)

	allContainers, err := s.cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return noop, fmt.Errorf("(*script).stopContainersAndServices: error querying for containers: %w", err)
	}
	containersToStop, err := s.cli.ContainerList(context.Background(), types.ContainerListOptions{
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: filterMatchLabel,
		}),
	})
	if err != nil {
		return noop, fmt.Errorf("(*script).stopContainersAndServices: error querying for containers to stop: %w", err)
	}

	var allServices []swarm.Service
	var servicesToScaleDown []handledSwarmService
	if isDockerSwarm {
		allServices, err = s.cli.ServiceList(context.Background(), types.ServiceListOptions{})
		if err != nil {
			return noop, fmt.Errorf("(*script).stopContainersAndServices: error querying for services: %w", err)
		}
		matchingServices, err := s.cli.ServiceList(context.Background(), types.ServiceListOptions{
			Filters: filters.NewArgs(filters.KeyValuePair{
				Key:   "label",
				Value: filterMatchLabel,
			}),
			Status: true,
		})
		for _, s := range matchingServices {
			servicesToScaleDown = append(servicesToScaleDown, handledSwarmService{
				serviceID:           s.ID,
				initialReplicaCount: *s.Spec.Mode.Replicated.Replicas,
			})
		}
		if err != nil {
			return noop, fmt.Errorf("(*script).stopContainersAndServices: error querying for services to scale down: %w", err)
		}
	}

	if len(containersToStop) == 0 && len(servicesToScaleDown) == 0 {
		return noop, nil
	}

	if isDockerSwarm {
		for _, container := range containersToStop {
			if swarmServiceID, ok := container.Labels["com.docker.swarm.service.id"]; ok {
				parentService, _, err := s.cli.ServiceInspectWithRaw(context.Background(), swarmServiceID, types.ServiceInspectOptions{})
				if err != nil {
					return noop, fmt.Errorf("(*script).stopContainersAndServices: error querying for parent service with ID %s: %w", swarmServiceID, err)
				}
				for label := range parentService.Spec.Labels {
					if label == "docker-volume-backup.stop-during-backup" {
						return noop, fmt.Errorf(
							"(*script).stopContainersAndServices: container %s is labeled to stop but has parent service %s which is also labeled, cannot continue",
							container.Names[0],
							parentService.Spec.Name,
						)
					}
				}
			}
		}
	}

	s.logger.Info(
		fmt.Sprintf(
			"Stopping %d out of %d running container(s) and scaling down %d out of %d active service(s) as they were labeled %s.",
			len(containersToStop),
			len(allContainers),
			len(servicesToScaleDown),
			len(allServices),
			filterMatchLabel,
		),
	)

	var stoppedContainers []types.Container
	var stopErrors []error
	for _, container := range containersToStop {
		if err := s.cli.ContainerStop(context.Background(), container.ID, ctr.StopOptions{}); err != nil {
			stopErrors = append(stopErrors, err)
		} else {
			stoppedContainers = append(stoppedContainers, container)
		}
	}

	var scaledDownServices []swarm.Service
	var scaleDownErrors concurrentSlice[error]
	if isDockerSwarm {
		wg := sync.WaitGroup{}
		for _, svc := range servicesToScaleDown {
			wg.Add(1)
			go func(svc handledSwarmService) {
				defer wg.Done()
				service, _, err := s.cli.ServiceInspectWithRaw(context.Background(), svc.serviceID, types.ServiceInspectOptions{})
				if err != nil {
					scaleDownErrors.append(
						fmt.Errorf("(*script).stopContainersAndServices: error inspecting service %s: %w", svc.serviceID, err),
					)
					return
				}
				var zero uint64 = 0
				serviceMode := &service.Spec.Mode
				switch {
				case serviceMode.Replicated != nil:
					serviceMode.Replicated.Replicas = &zero
				default:
					scaleDownErrors.append(
						fmt.Errorf("(*script).stopContainersAndServices: labeled service %s has to be in replicated mode", service.Spec.Name),
					)
					return
				}

				response, err := s.cli.ServiceUpdate(context.Background(), service.ID, service.Version, service.Spec, types.ServiceUpdateOptions{})
				if err != nil {
					scaleDownErrors.append(err)
					return
				}

				for _, warning := range response.Warnings {
					s.logger.Warn(
						fmt.Sprintf("The Docker API returned a warning when scaling down service %s: %s", service.Spec.Name, warning),
					)
				}

				if err := progress.ServiceProgress(context.Background(), s.cli, service.ID, discardWriter); err != nil {
					scaleDownErrors.append(err)
				} else {
					scaledDownServices = append(scaledDownServices, service)
				}

				// progress.ServiceProgress returns too early, so we need to manually check
				// whether all containers belonging to the service have actually been removed
				for {
					containers, err := s.cli.ContainerList(context.Background(), types.ContainerListOptions{
						Filters: filters.NewArgs(filters.KeyValuePair{
							Key:   "label",
							Value: fmt.Sprintf("com.docker.swarm.service.id=%s", service.ID),
						}),
					})
					if err != nil {
						scaleDownErrors.append(err)
						break
					}
					if len(containers) == 0 {
						break
					}
					time.Sleep(time.Second)
				}
			}(svc)
		}
		wg.Wait()
	}

	s.stats.Containers = ContainersStats{
		All:        uint(len(allContainers)),
		ToStop:     uint(len(containersToStop)),
		Stopped:    uint(len(stoppedContainers)),
		StopErrors: uint(len(stopErrors)),
	}

	s.stats.Services = ServicesStats{
		All:             uint(len(allServices)),
		ToScaleDown:     uint(len(servicesToScaleDown)),
		ScaledDown:      uint(len(scaledDownServices)),
		ScaleDownErrors: uint(len(scaleDownErrors.value())),
	}

	var initialErr error
	allErrors := append(stopErrors, scaleDownErrors.value()...)
	if len(allErrors) != 0 {
		initialErr = fmt.Errorf(
			"(*script).stopContainersAndServices: %d error(s) stopping containers: %w",
			len(allErrors),
			errors.Join(allErrors...),
		)
	}

	return func() error {
		servicesRequiringForceUpdate := map[string]struct{}{}

		var restartErrors []error
		for _, container := range stoppedContainers {
			if swarmServiceName, ok := container.Labels["com.docker.swarm.service.name"]; ok {
				servicesRequiringForceUpdate[swarmServiceName] = struct{}{}
				continue
			}
			if err := s.cli.ContainerStart(context.Background(), container.ID, types.ContainerStartOptions{}); err != nil {
				restartErrors = append(restartErrors, err)
			}
		}

		if len(servicesRequiringForceUpdate) != 0 {
			services, _ := s.cli.ServiceList(context.Background(), types.ServiceListOptions{})
			for serviceName := range servicesRequiringForceUpdate {
				var serviceMatch swarm.Service
				for _, service := range services {
					if service.Spec.Name == serviceName {
						serviceMatch = service
						break
					}
				}
				if serviceMatch.ID == "" {
					restartErrors = append(
						restartErrors,
						fmt.Errorf("(*script).stopContainersAndServices: couldn't find service with name %s", serviceName),
					)
					continue
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

		var scaleUpErrors concurrentSlice[error]
		if isDockerSwarm {
			wg := &sync.WaitGroup{}
			for _, svc := range servicesToScaleDown {
				wg.Add(1)
				go func(svc handledSwarmService) {
					defer wg.Done()
					service, _, err := s.cli.ServiceInspectWithRaw(context.Background(), svc.serviceID, types.ServiceInspectOptions{})
					if err != nil {
						scaleUpErrors.append(err)
						return
					}

					service.Spec.Mode.Replicated.Replicas = &svc.initialReplicaCount
					response, err := s.cli.ServiceUpdate(
						context.Background(),
						service.ID,
						service.Version, service.Spec,
						types.ServiceUpdateOptions{},
					)
					if err != nil {
						scaleUpErrors.append(err)
						return
					}
					for _, warning := range response.Warnings {
						s.logger.Warn(
							fmt.Sprintf("The Docker API returned a warning when scaling up service %s: %s", service.Spec.Name, warning),
						)
					}
					if err := progress.ServiceProgress(context.Background(), s.cli, service.ID, discardWriter); err != nil {
						scaleUpErrors.append(err)
					}
				}(svc)
			}
			wg.Wait()
		}

		allErrors := append(restartErrors, scaleUpErrors.value()...)
		if len(allErrors) != 0 {
			return fmt.Errorf(
				"stopContainers: %d error(s) restarting containers and services: %w",
				len(allErrors),
				errors.Join(allErrors...),
			)
		}
		s.logger.Info(
			fmt.Sprintf(
				"Restarted %d container(s) and %d service(s).",
				len(stoppedContainers),
				len(scaledDownServices),
			),
		)
		return nil
	}, initialErr
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

// must exits the script run prematurely in case the given error
// is non-nil.
func (s *script) must(err error) {
	if err != nil {
		s.logger.Error(
			fmt.Sprintf("Fatal error running backup: %s", err),
		)
		panic(err)
	}
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
