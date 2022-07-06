// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/kelseyhightower/envconfig"
	"github.com/leekchan/timeutil"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/otiai10/copy"
	"github.com/sirupsen/logrus"
	"github.com/studio-b12/gowebdav"
	"golang.org/x/crypto/openpgp"
)

// script holds all the stateful information required to orchestrate a
// single backup run.
type script struct {
	cli          *client.Client
	minioClient  *minio.Client
	webdavClient *gowebdav.Client
	sshClient    *ssh.Client
	sftpClient   *sftp.Client
	logger       *logrus.Logger
	sender       *router.ServiceRouter
	template     *template.Template
	hooks        []hook
	hookLevel    hookLevel

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
			Storages:  StoragesStats{},
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

	if s.c.AwsS3BucketName != "" {
		var creds *credentials.Credentials
		if s.c.AwsAccessKeyID != "" && s.c.AwsSecretAccessKey != "" {
			creds = credentials.NewStaticV4(
				s.c.AwsAccessKeyID,
				s.c.AwsSecretAccessKey,
				"",
			)
		} else if s.c.AwsIamRoleEndpoint != "" {
			creds = credentials.NewIAM(s.c.AwsIamRoleEndpoint)
		} else {
			return nil, errors.New("newScript: AWS_S3_BUCKET_NAME is defined, but no credentials were provided")
		}

		options := minio.Options{
			Creds:  creds,
			Secure: s.c.AwsEndpointProto == "https",
		}

		if s.c.AwsEndpointInsecure {
			if !options.Secure {
				return nil, errors.New("newScript: AWS_ENDPOINT_INSECURE = true is only meaningful for https")
			}

			transport, err := minio.DefaultTransport(true)
			if err != nil {
				return nil, fmt.Errorf("newScript: failed to create default minio transport")
			}
			transport.TLSClientConfig.InsecureSkipVerify = true
			options.Transport = transport
		}

		mc, err := minio.New(s.c.AwsEndpoint, &options)
		if err != nil {
			return nil, fmt.Errorf("newScript: error setting up minio client: %w", err)
		}
		s.minioClient = mc
	}

	if s.c.WebdavUrl != "" {
		if s.c.WebdavUsername == "" || s.c.WebdavPassword == "" {
			return nil, errors.New("newScript: WEBDAV_URL is defined, but no credentials were provided")
		} else {
			webdavClient := gowebdav.NewClient(s.c.WebdavUrl, s.c.WebdavUsername, s.c.WebdavPassword)
			s.webdavClient = webdavClient
			if s.c.WebdavUrlInsecure {
				defaultTransport, ok := http.DefaultTransport.(*http.Transport)
				if !ok {
					return nil, errors.New("newScript: unexpected error when asserting type for http.DefaultTransport")
				}
				webdavTransport := defaultTransport.Clone()
				webdavTransport.TLSClientConfig.InsecureSkipVerify = s.c.WebdavUrlInsecure
				s.webdavClient.SetTransport(webdavTransport)
			}
		}
	}

	if s.c.SSHHostName != "" {
		var authMethods []ssh.AuthMethod

		if s.c.SSHPassword != "" {
			authMethods = append(authMethods, ssh.Password(s.c.SSHPassword))
		}

		if _, err := os.Stat(s.c.SSHIdentityFile); err == nil {
			key, err := ioutil.ReadFile(s.c.SSHIdentityFile)
			if err != nil {
				return nil, errors.New("newScript: error reading the private key")
			}

			var signer ssh.Signer
			if s.c.SSHIdentityPassphrase != "" {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(s.c.SSHIdentityPassphrase))
				if err != nil {
					return nil, errors.New("newScript: error parsing the encrypted private key")
				}
				authMethods = append(authMethods, ssh.PublicKeys(signer))
			} else {
				signer, err = ssh.ParsePrivateKey(key)
				if err != nil {
					return nil, errors.New("newScript: error parsing the private key")
				}
				authMethods = append(authMethods, ssh.PublicKeys(signer))
			}
		}

		sshClientConfig := &ssh.ClientConfig{
			User:            s.c.SSHUser,
			Auth:            authMethods,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}
		sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", s.c.SSHHostName, s.c.SSHPort), sshClientConfig)
		s.sshClient = sshClient
		if err != nil {
			return nil, fmt.Errorf("newScript: error creating ssh client: %w", err)
		}
		_, _, err = s.sshClient.SendRequest("keepalive", false, nil)
		if err != nil {
			return nil, err
		}

		sftpClient, err := sftp.NewClient(sshClient)
		s.sftpClient = sftpClient
		if err != nil {
			return nil, fmt.Errorf("newScript: error creating sftp client: %w", err)
		}
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

func (s *script) runCommands() (func() error, error) {
	if s.cli == nil {
		return noop, nil
	}

	if err := s.runLabeledCommands("docker-volume-backup.exec-pre"); err != nil {
		return noop, fmt.Errorf("runCommands: error running pre commands: %w", err)
	}
	return func() error {
		if err := s.runLabeledCommands("docker-volume-backup.exec-post"); err != nil {
			return fmt.Errorf("runCommands: error running post commands: %w", err)
		}
		return nil
	}, nil
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
		return noop, fmt.Errorf("stopContainersAndRun: error querying for containers: %w", err)
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
		return noop, fmt.Errorf("stopContainersAndRun: error querying for containers to stop: %w", err)
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
			"stopContainersAndRun: %d error(s) stopping containers: %w",
			len(stopErrors),
			join(stopErrors...),
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
					return fmt.Errorf("stopContainersAndRun: couldn't find service with name %s", serviceName)
				}
				serviceMatch.Spec.TaskTemplate.ForceUpdate = 1
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
				"stopContainersAndRun: %d error(s) restarting containers and services: %w",
				len(restartErrors),
				join(restartErrors...),
			)
		}
		s.logger.Infof(
			"Restarted %d container(s) and the matching service(s).",
			len(stoppedContainers),
		)
		return nil
	}, stopError
}

// takeBackup creates a tar archive of the configured backup location and
// saves it to disk.
func (s *script) takeBackup() error {
	backupSources := s.c.BackupSources

	if s.c.BackupFromSnapshot {
		s.logger.Warn(
			"Using BACKUP_FROM_SNAPSHOT has been deprecated and will be removed in the next major version.",
		)
		s.logger.Warn(
			"Please use `exec-pre` and `exec-post` commands to prepare your backup sources. Refer to the README for an upgrade guide.",
		)
		backupSources = filepath.Join("/tmp", s.c.BackupSources)
		// copy before compressing guard against a situation where backup folder's content are still growing.
		s.registerHook(hookLevelPlumbing, func(error) error {
			if err := remove(backupSources); err != nil {
				return fmt.Errorf("takeBackup: error removing snapshot: %w", err)
			}
			s.logger.Infof("Removed snapshot `%s`.", backupSources)
			return nil
		})
		if err := copy.Copy(s.c.BackupSources, backupSources, copy.Options{
			PreserveTimes: true,
			PreserveOwner: true,
		}); err != nil {
			return fmt.Errorf("takeBackup: error creating snapshot: %w", err)
		}
		s.logger.Infof("Created snapshot of `%s` at `%s`.", s.c.BackupSources, backupSources)
	}

	tarFile := s.file
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(tarFile); err != nil {
			return fmt.Errorf("takeBackup: error removing tar file: %w", err)
		}
		s.logger.Infof("Removed tar file `%s`.", tarFile)
		return nil
	})

	backupPath, err := filepath.Abs(stripTrailingSlashes(backupSources))
	if err != nil {
		return fmt.Errorf("takeBackup: error getting absolute path: %w", err)
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
		return fmt.Errorf("compress: error walking filesystem tree: %w", err)
	}

	if err := createArchive(filesEligibleForBackup, backupSources, tarFile); err != nil {
		return fmt.Errorf("takeBackup: error compressing backup folder: %w", err)
	}

	s.logger.Infof("Created backup of `%s` at `%s`.", backupSources, tarFile)
	return nil
}

// encryptBackup encrypts the backup file using PGP and the configured passphrase.
// In case no passphrase is given it returns early, leaving the backup file
// untouched.
func (s *script) encryptBackup() error {
	if s.c.GpgPassphrase == "" {
		return nil
	}

	gpgFile := fmt.Sprintf("%s.gpg", s.file)
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(gpgFile); err != nil {
			return fmt.Errorf("encryptBackup: error removing gpg file: %w", err)
		}
		s.logger.Infof("Removed GPG file `%s`.", gpgFile)
		return nil
	})

	outFile, err := os.Create(gpgFile)
	defer outFile.Close()
	if err != nil {
		return fmt.Errorf("encryptBackup: error opening out file: %w", err)
	}

	_, name := path.Split(s.file)
	dst, err := openpgp.SymmetricallyEncrypt(outFile, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
		IsBinary: true,
		FileName: name,
	}, nil)
	defer dst.Close()
	if err != nil {
		return fmt.Errorf("encryptBackup: error encrypting backup file: %w", err)
	}

	src, err := os.Open(s.file)
	if err != nil {
		return fmt.Errorf("encryptBackup: error opening backup file `%s`: %w", s.file, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("encryptBackup: error writing ciphertext to file: %w", err)
	}

	s.file = gpgFile
	s.logger.Infof("Encrypted backup using given passphrase, saving as `%s`.", s.file)
	return nil
}

// copyBackup makes sure the backup file is copied to both local and remote locations
// as per the given configuration.
func (s *script) copyBackup() error {
	_, name := path.Split(s.file)
	if stat, err := os.Stat(s.file); err != nil {
		return fmt.Errorf("copyBackup: unable to stat backup file: %w", err)
	} else {
		size := stat.Size()
		s.stats.BackupFile = BackupFileStats{
			Size:     uint64(size),
			Name:     name,
			FullPath: s.file,
		}
	}

	if s.minioClient != nil {
		if _, err := s.minioClient.FPutObject(context.Background(), s.c.AwsS3BucketName, filepath.Join(s.c.AwsS3Path, name), s.file, minio.PutObjectOptions{
			ContentType:  "application/tar+gzip",
			StorageClass: s.c.AwsStorageClass,
		}); err != nil {
			return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
		}
		s.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`.", s.file, s.c.AwsS3BucketName)
	}

	if s.webdavClient != nil {
		bytes, err := os.ReadFile(s.file)
		if err != nil {
			return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
		}
		if err := s.webdavClient.MkdirAll(s.c.WebdavPath, 0644); err != nil {
			return fmt.Errorf("copyBackup: error creating directory '%s' on WebDAV server: %w", s.c.WebdavPath, err)
		}
		if err := s.webdavClient.Write(filepath.Join(s.c.WebdavPath, name), bytes, 0644); err != nil {
			return fmt.Errorf("copyBackup: error uploading the file to WebDAV server: %w", err)
		}
		s.logger.Infof("Uploaded a copy of backup `%s` to WebDAV-URL '%s' at path '%s'.", s.file, s.c.WebdavUrl, s.c.WebdavPath)
	}

	if s.sshClient != nil {
		source, err := os.Open(s.file)
		if err != nil {
			return fmt.Errorf("copyBackup: error reading the file to be uploaded: %w", err)
		}
		defer source.Close()

		destination, err := s.sftpClient.Create(filepath.Join(s.c.SSHRemotePath, name))
		if err != nil {
			return fmt.Errorf("copyBackup: error creating file on SSH storage: %w", err)
		}
		defer destination.Close()

		chunk := make([]byte, 1000000)
		for {
			num, err := source.Read(chunk)
			if err == io.EOF {
				tot, err := destination.Write(chunk[:num])
				if err != nil {
					return fmt.Errorf("copyBackup: error uploading the file to SSH storage: %w", err)
				}

				if tot != len(chunk[:num]) {
					return fmt.Errorf("sshClient: failed to write stream")
				}

				break
			}

			if err != nil {
				return fmt.Errorf("copyBackup: error uploading the file to SSH storage: %w", err)
			}

			tot, err := destination.Write(chunk[:num])
			if err != nil {
				return fmt.Errorf("copyBackup: error uploading the file to SSH storage: %w", err)
			}

			if tot != len(chunk[:num]) {
				return fmt.Errorf("sshClient: failed to write stream")
			}
		}

		s.logger.Infof("Uploaded a copy of backup `%s` to SSH storage '%s' at path '%s'.", s.file, s.c.SSHHostName, s.c.SSHRemotePath)
	}

	if _, err := os.Stat(s.c.BackupArchive); !os.IsNotExist(err) {
		if err := copyFile(s.file, path.Join(s.c.BackupArchive, name)); err != nil {
			return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
		}
		s.logger.Infof("Stored copy of backup `%s` in local archive `%s`.", s.file, s.c.BackupArchive)
		if s.c.BackupLatestSymlink != "" {
			symlink := path.Join(s.c.BackupArchive, s.c.BackupLatestSymlink)
			if _, err := os.Lstat(symlink); err == nil {
				os.Remove(symlink)
			}
			if err := os.Symlink(name, symlink); err != nil {
				return fmt.Errorf("copyBackup: error creating latest symlink: %w", err)
			}
			s.logger.Infof("Created/Updated symlink `%s` for latest backup.", s.c.BackupLatestSymlink)
		}
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

	// doPrune holds general control flow that applies to any kind of storage.
	// Callers can pass in a thunk that performs the actual deletion of files.
	var doPrune = func(lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
		if lenMatches != 0 && lenMatches != lenCandidates {
			if err := doRemoveFiles(); err != nil {
				return err
			}
			s.logger.Infof(
				"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
				lenMatches,
				lenCandidates,
				description,
				s.c.BackupRetentionDays,
			)
		} else if lenMatches != 0 && lenMatches == lenCandidates {
			s.logger.Warnf("The current configuration would delete all %d existing %s.", lenMatches, description)
			s.logger.Warn("Refusing to do so, please check your configuration.")
		} else {
			s.logger.Infof("None of %d existing %s were pruned.", lenCandidates, description)
		}
		return nil
	}

	if s.minioClient != nil {
		candidates := s.minioClient.ListObjects(context.Background(), s.c.AwsS3BucketName, minio.ListObjectsOptions{
			WithMetadata: true,
			Prefix:       filepath.Join(s.c.AwsS3Path, s.c.BackupPruningPrefix),
			Recursive:    true,
		})

		var matches []minio.ObjectInfo
		var lenCandidates int
		for candidate := range candidates {
			lenCandidates++
			if candidate.Err != nil {
				return fmt.Errorf(
					"pruneBackups: error looking up candidates from remote storage: %w",
					candidate.Err,
				)
			}
			if candidate.LastModified.Before(deadline) {
				matches = append(matches, candidate)
			}
		}

		s.stats.Storages.S3 = StorageStats{
			Total:  uint(lenCandidates),
			Pruned: uint(len(matches)),
		}

		doPrune(len(matches), lenCandidates, "remote backup(s)", func() error {
			objectsCh := make(chan minio.ObjectInfo)
			go func() {
				for _, match := range matches {
					objectsCh <- match
				}
				close(objectsCh)
			}()
			errChan := s.minioClient.RemoveObjects(context.Background(), s.c.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
			var removeErrors []error
			for result := range errChan {
				if result.Err != nil {
					removeErrors = append(removeErrors, result.Err)
				}
			}
			if len(removeErrors) != 0 {
				return join(removeErrors...)
			}
			return nil
		})
	}

	if s.webdavClient != nil {
		candidates, err := s.webdavClient.ReadDir(s.c.WebdavPath)
		if err != nil {
			return fmt.Errorf("pruneBackups: error looking up candidates from remote storage: %w", err)
		}
		var matches []fs.FileInfo
		var lenCandidates int
		for _, candidate := range candidates {
			if !strings.HasPrefix(candidate.Name(), s.c.BackupPruningPrefix) {
				continue
			}
			lenCandidates++
			if candidate.ModTime().Before(deadline) {
				matches = append(matches, candidate)
			}
		}

		s.stats.Storages.WebDAV = StorageStats{
			Total:  uint(lenCandidates),
			Pruned: uint(len(matches)),
		}

		doPrune(len(matches), lenCandidates, "WebDAV backup(s)", func() error {
			for _, match := range matches {
				if err := s.webdavClient.Remove(filepath.Join(s.c.WebdavPath, match.Name())); err != nil {
					return fmt.Errorf("pruneBackups: error removing file from WebDAV storage: %w", err)
				}
			}
			return nil
		})
	}

	if s.sshClient != nil {
		candidates, err := s.sftpClient.ReadDir(s.c.SSHRemotePath)
		if err != nil {
			return fmt.Errorf("pruneBackups: error reading directory from SSH storage: %w", err)
		}

		var matches []string
		for _, candidate := range candidates {
			if !strings.HasPrefix(candidate.Name(), s.c.BackupPruningPrefix) {
				continue
			}
			if candidate.ModTime().Before(deadline) {
				matches = append(matches, candidate.Name())
			}
		}

		s.stats.Storages.SSH = StorageStats{
			Total:  uint(len(candidates)),
			Pruned: uint(len(matches)),
		}

		doPrune(len(matches), len(candidates), "SSH backup(s)", func() error {
			for _, match := range matches {
				if err := s.sftpClient.Remove(filepath.Join(s.c.SSHRemotePath, match)); err != nil {
					return fmt.Errorf("pruneBackups: error removing file from SSH storage: %w", err)
				}
			}
			return nil
		})
	}

	if _, err := os.Stat(s.c.BackupArchive); !os.IsNotExist(err) {
		globPattern := path.Join(
			s.c.BackupArchive,
			fmt.Sprintf("%s*", s.c.BackupPruningPrefix),
		)
		globMatches, err := filepath.Glob(globPattern)
		if err != nil {
			return fmt.Errorf(
				"pruneBackups: error looking up matching files using pattern %s: %w",
				globPattern,
				err,
			)
		}

		var candidates []string
		for _, candidate := range globMatches {
			fi, err := os.Lstat(candidate)
			if err != nil {
				return fmt.Errorf(
					"pruneBackups: error calling Lstat on file %s: %w",
					candidate,
					err,
				)
			}

			if fi.Mode()&os.ModeSymlink != os.ModeSymlink {
				candidates = append(candidates, candidate)
			}
		}

		var matches []string
		for _, candidate := range candidates {
			fi, err := os.Stat(candidate)
			if err != nil {
				return fmt.Errorf(
					"pruneBackups: error calling stat on file %s: %w",
					candidate,
					err,
				)
			}
			if fi.ModTime().Before(deadline) {
				matches = append(matches, candidate)
			}
		}

		s.stats.Storages.Local = StorageStats{
			Total:  uint(len(candidates)),
			Pruned: uint(len(matches)),
		}

		doPrune(len(matches), len(candidates), "local backup(s)", func() error {
			var removeErrors []error
			for _, match := range matches {
				if err := os.Remove(match); err != nil {
					removeErrors = append(removeErrors, err)
				}
			}
			if len(removeErrors) != 0 {
				return fmt.Errorf(
					"pruneBackups: %d error(s) deleting local files, starting with: %w",
					len(removeErrors),
					join(removeErrors...),
				)
			}
			return nil
		})
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
