// Copyright 2021 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	sTypes "github.com/containrrr/shoutrrr/pkg/types"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/gofrs/flock"
	"github.com/kelseyhightower/envconfig"
	"github.com/leekchan/timeutil"
	"github.com/m90/targz"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/otiai10/copy"
	"github.com/sirupsen/logrus"
	"github.com/studio-b12/gowebdav"
	"golang.org/x/crypto/openpgp"
)

//go:embed notifications.tmpl
var defaultNotifications string

func main() {
	unlock := lock("/var/lock/dockervolumebackup.lock")
	defer unlock()

	s, err := newScript()
	if err != nil {
		panic(err)
	}

	defer func() {
		if pArg := recover(); pArg != nil {
			if err, ok := pArg.(error); ok {
				if hookErr := s.runHooks(err); hookErr != nil {
					s.logger.Errorf("An error occurred calling the registered hooks: %s", hookErr)
				}
				os.Exit(1)
			}
			panic(pArg)
		}

		if err := s.runHooks(nil); err != nil {
			s.logger.Errorf(
				"Backup procedure ran successfully, but an error ocurred calling the registered hooks: %v",
				err,
			)
			os.Exit(1)
		}
		s.logger.Info("Finished running backup tasks.")
	}()

	s.must(func() error {
		restartContainers, err := s.stopContainers()
		// The mechanism for restarting containers is not using hooks as it
		// should happen as soon as possible (i.e. before uploading backups or
		// similar).
		defer func() {
			s.must(restartContainers())
		}()
		if err != nil {
			return err
		}
		return s.takeBackup()
	}())

	s.must(s.encryptBackup())
	s.must(s.copyBackup())
	s.stats.EndTime = time.Now()
	s.stats.TookTime = s.stats.EndTime.Sub(s.stats.EndTime)
	s.must(s.pruneOldBackups())
}

// script holds all the stateful information required to orchestrate a
// single backup run.
type script struct {
	cli          *client.Client
	minioClient  *minio.Client
	webdavClient *gowebdav.Client
	logger       *logrus.Logger
	sender       *router.ServiceRouter
	template     *template.Template
	hooks        []hook
	hookLevel    hookLevel

	file  string
	stats *Stats

	c *Config
}

// ContainersStats stats about the docker containers
type ContainersStats struct {
	All        uint
	ToStop     uint
	Stopped    uint
	StopErrors uint
}

// BackupFileStats stats about the created backup file
type BackupFileStats struct {
	Name     string
	FullPath string
	Size     uint64
}

// ArchiveStats stats about the status of an archival directory
type ArchiveStats struct {
	Total       uint
	Pruned      uint
	PruneErrors uint
}

// ArchivesStats stats about each possible archival location (Local, WebDAV, S3)
type ArchivesStats struct {
	Local  ArchiveStats
	WebDAV ArchiveStats
	S3     ArchiveStats
}

// Stats global stats regarding script execution
type Stats struct {
	StartTime  time.Time
	EndTime    time.Time
	TookTime   time.Duration
	LogOutput  *bytes.Buffer
	Containers ContainersStats
	BackupFile BackupFileStats
	Archives   ArchivesStats
}

// NotificationData data to be passed to the notification templates
type NotificationData struct {
	Error  error
	Config *Config
	Stats  *Stats
}

type Config struct {
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
	AwsS3BucketName            string        `split_words:"true"`
	AwsS3Path                  string        `split_words:"true"`
	AwsEndpoint                string        `split_words:"true" default:"s3.amazonaws.com"`
	AwsEndpointProto           string        `split_words:"true" default:"https"`
	AwsEndpointInsecure        bool          `split_words:"true"`
	AwsAccessKeyID             string        `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey         string        `split_words:"true"`
	AwsIamRoleEndpoint         string        `split_words:"true"`
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
	WebdavPath                 string        `split_words:"true" default:"/"`
	WebdavUsername             string        `split_words:"true"`
	WebdavPassword             string        `split_words:"true"`
}

var msgBackupFailed = "backup run failed"

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
			Archives:  ArchivesStats{},
		},
	}

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
	if !os.IsNotExist(err) {
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

	tmpl := template.New("")
	tmpl.Funcs(template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format(time.RFC3339)
		},
		"formatBytesDec": func(bytes uint64) string {
			return formatBytes(bytes, true)
		},
		"formatBytesBin": func(bytes uint64) string {
			return formatBytes(bytes, false)
		},
	})
	tmpl, err = tmpl.Parse(defaultNotifications)
	if err != nil {
		return nil, fmt.Errorf("newScript: unable to parse default notifications templates: %w", err)
	}

	if _, err := os.Stat("/etc/dockervolumebackup/notifications.d"); err == nil {
		tmpl, err = tmpl.ParseGlob("/etc/dockervolumebackup/notifications.d/*.*")
		if err != nil {
			return nil, fmt.Errorf("newScript: unable to parse user defined notifications templates: %w", err)
		}
	}
	s.template = tmpl

	return s, nil
}

var noop = func() error { return nil }

// registerHook adds the given action at the given level.
func (s *script) registerHook(level hookLevel, action func(err error) error) {
	s.hooks = append(s.hooks, hook{level, action})
}

// notify sends a notification using the given title and body templates.
// Automatically creates notification data, adding the given error
func (s *script) notify(titleTemplate string, bodyTemplate string, err error) error {
	params := NotificationData{
		Error:  err,
		Stats:  s.stats,
		Config: s.c,
	}

	titleBuf := &bytes.Buffer{}
	if err := s.template.ExecuteTemplate(titleBuf, titleTemplate, params); err != nil {
		return fmt.Errorf("notifyFailure: error executing %s template: %w", titleTemplate, err)
	}

	bodyBuf := &bytes.Buffer{}
	if err := s.template.ExecuteTemplate(bodyBuf, bodyTemplate, params); err != nil {
		return fmt.Errorf("notifyFailure: error executing %s template: %w", bodyTemplate, err)
	}

	if err := s.sendNotification(titleBuf.String(), bodyBuf.String()); err != nil {
		return fmt.Errorf("notifyFailure: error notifying: %w", err)
	}
	return nil
}

// notifyFailure sends a notification about a failed backup run
func (s *script) notifyFailure(err error) error {
	return s.notify("title_failure", "body_failure", err)
}

// notifyFailure sends a notification about a successful backup run
func (s *script) notifySuccess() error {
	return s.notify("title_success", "body_success", nil)
}

// sendNotification sends a notification to all configured third party services
func (s *script) sendNotification(title, body string) error {
	var errs []error
	for _, result := range s.sender.Send(body, &sTypes.Params{"title": title}) {
		if result != nil {
			errs = append(errs, result)
		}
	}
	if len(errs) != 0 {
		return fmt.Errorf("sendNotification: error sending message: %w", join(errs...))
	}
	return nil
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
	if err := targz.Compress(backupSources, tarFile); err != nil {
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
			ContentType: "application/tar+gzip",
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

// pruneOldBackups rotates away backups from local and remote storages using
// the given configuration. In case the given configuration would delete all
// backups, it does nothing instead.
func (s *script) pruneOldBackups() error {
	if s.c.BackupRetentionDays < 0 {
		return nil
	}

	if s.c.BackupPruningLeeway != 0 {
		s.logger.Infof("Sleeping for %s before pruning backups.", s.c.BackupPruningLeeway)
		time.Sleep(s.c.BackupPruningLeeway)
	}

	deadline := time.Now().AddDate(0, 0, -int(s.c.BackupRetentionDays))

	// Prune minio/S3 backups
	if s.minioClient != nil {
		candidates := s.minioClient.ListObjects(context.Background(), s.c.AwsS3BucketName, minio.ListObjectsOptions{
			WithMetadata: true,
			Prefix:       s.c.BackupPruningPrefix,
		})

		var matches []minio.ObjectInfo
		var lenCandidates int
		for candidate := range candidates {
			lenCandidates++
			if candidate.Err != nil {
				return fmt.Errorf(
					"pruneOldBackups: error looking up candidates from remote storage: %w",
					candidate.Err,
				)
			}
			if candidate.LastModified.Before(deadline) {
				matches = append(matches, candidate)
			}
		}

		s.stats.Archives.S3 = ArchiveStats{
			Total:  uint(lenCandidates),
			Pruned: uint(len(matches)),
		}
		if len(matches) != 0 && len(matches) != lenCandidates {
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
			s.stats.Archives.S3.PruneErrors = uint(len(removeErrors))

			if len(removeErrors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d error(s) removing files from remote storage: %w",
					len(removeErrors),
					join(removeErrors...),
				)
			}

			s.logger.Infof(
				"Pruned %d out of %d remote backup(s) as their age exceeded the configured retention period of %d days.",
				s.stats.Archives.S3.Pruned,
				s.stats.Archives.S3.Total,
				s.c.BackupRetentionDays,
			)
		} else if len(matches) != 0 && len(matches) == lenCandidates {
			s.logger.Warnf(
				"The current configuration would delete all %d remote backup copies.",
				len(matches),
			)
			s.logger.Warn("Refusing to do so, please check your configuration.")
		} else {
			s.logger.Infof("None of %d remote backup(s) were pruned.", lenCandidates)
		}
	}

	// Prune WebDAV backups
	if s.webdavClient != nil {
		candidates, err := s.webdavClient.ReadDir(s.c.WebdavPath)
		if err != nil {
			return fmt.Errorf("pruneOldBackups: error looking up candidates from remote storage: %w", err)
		}
		var matches []fs.FileInfo
		var lenCandidates int
		for _, candidate := range candidates {
			lenCandidates++
			if candidate.ModTime().Before(deadline) {
				matches = append(matches, candidate)
			}
		}

		s.stats.Archives.WebDAV = ArchiveStats{
			Total:  uint(lenCandidates),
			Pruned: uint(len(matches)),
		}
		if len(matches) != 0 && len(matches) != lenCandidates {
			var removeErrors []error
			for _, match := range matches {
				if err := s.webdavClient.Remove(filepath.Join(s.c.WebdavPath, match.Name())); err != nil {
					removeErrors = append(removeErrors, err)
				} else {
					s.logger.Infof("Pruned %s from WebDAV: %s", match.Name(), filepath.Join(s.c.WebdavUrl, s.c.WebdavPath))
				}
			}
			s.stats.Archives.WebDAV.PruneErrors = uint(len(removeErrors))
			if len(removeErrors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d error(s) removing files from remote storage: %w",
					len(removeErrors),
					join(removeErrors...),
				)
			}
			s.logger.Infof(
				"Pruned %d out of %d remote backup(s) as their age exceeded the configured retention period of %d days.",
				s.stats.Archives.WebDAV.Pruned,
				s.stats.Archives.WebDAV.Total,
				s.c.BackupRetentionDays,
			)
		} else if len(matches) != 0 && len(matches) == lenCandidates {
			s.logger.Warnf("The current configuration would delete all %d remote backup copies.", len(matches))
			s.logger.Warn("Refusing to do so, please check your configuration.")
		} else {
			s.logger.Infof("None of %d remote backup(s) were pruned.", lenCandidates)
		}
	}

	// Prune local backups
	if _, err := os.Stat(s.c.BackupArchive); !os.IsNotExist(err) {
		globPattern := path.Join(
			s.c.BackupArchive,
			fmt.Sprintf("%s*", s.c.BackupPruningPrefix),
		)
		globMatches, err := filepath.Glob(globPattern)
		if err != nil {
			return fmt.Errorf(
				"pruneOldBackups: error looking up matching files using pattern %s: %w",
				globPattern,
				err,
			)
		}

		var candidates []string
		for _, candidate := range globMatches {
			fi, err := os.Lstat(candidate)
			if err != nil {
				return fmt.Errorf(
					"pruneOldBackups: error calling Lstat on file %s: %w",
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
					"pruneOldBackups: error calling stat on file %s: %w",
					candidate,
					err,
				)
			}
			if fi.ModTime().Before(deadline) {
				matches = append(matches, candidate)
			}
		}

		s.stats.Archives.Local = ArchiveStats{
			Total:  uint(len(candidates)),
			Pruned: uint(len(matches)),
		}
		if len(matches) != 0 && len(matches) != len(candidates) {
			var removeErrors []error
			for _, match := range matches {
				if err := os.Remove(match); err != nil {
					removeErrors = append(removeErrors, err)
				}
			}
			if len(removeErrors) != 0 {
				s.stats.Archives.Local.PruneErrors = uint(len(removeErrors))
				return fmt.Errorf(
					"pruneOldBackups: %d error(s) deleting local files, starting with: %w",
					len(removeErrors),
					join(removeErrors...),
				)
			}
			s.logger.Infof(
				"Pruned %d out of %d local backup(s) as their age exceeded the configured retention period of %d days.",
				s.stats.Archives.Local.Pruned,
				s.stats.Archives.Local.Total,
				s.c.BackupRetentionDays,
			)
		} else if len(matches) != 0 && len(matches) == len(candidates) {
			s.logger.Warnf(
				"The current configuration would delete all %d local backup copies.",
				len(matches),
			)
			s.logger.Warn("Refusing to do so, please check your configuration.")
		} else {
			s.logger.Infof("None of %d local backup(s) were pruned.", len(candidates))
		}
	}
	return nil
}

// runHooks runs all hooks that have been registered using the
// given levels in the defined ordering. In case executing a hook returns an
// error, the following hooks will still be run before the function returns.
func (s *script) runHooks(err error) error {
	sort.SliceStable(s.hooks, func(i, j int) bool {
		return s.hooks[i].level < s.hooks[j].level
	})
	var actionErrors []error
	for _, hook := range s.hooks {
		if hook.level > s.hookLevel {
			continue
		}
		if actionErr := hook.action(err); actionErr != nil {
			actionErrors = append(actionErrors, fmt.Errorf("runHooks: error running hook: %w", actionErr))
		}
	}
	if len(actionErrors) != 0 {
		return join(actionErrors...)
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

// remove removes the given file or directory from disk.
func remove(location string) error {
	fi, err := os.Lstat(location)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove: error checking for existence of `%s`: %w", location, err)
	}
	if fi.IsDir() {
		err = os.RemoveAll(location)
	} else {
		err = os.Remove(location)
	}
	if err != nil {
		return fmt.Errorf("remove: error removing `%s`: %w", location, err)
	}
	return nil
}

// lock opens a lockfile at the given location, keeping it locked until the
// caller invokes the returned release func. When invoked while the file is
// still locked the function panics.
func lock(lockfile string) func() error {
	fileLock := flock.New(lockfile)
	acquired, err := fileLock.TryLock()
	if err != nil {
		panic(err)
	}
	if !acquired {
		panic("unable to acquire file lock")
	}
	return fileLock.Unlock
}

// copy creates a copy of the file located at `dst` at `src`.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// join takes a list of errors and joins them into a single error
func join(errs ...error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	var msgs []string
	for _, err := range errs {
		if err == nil {
			continue
		}
		msgs = append(msgs, err.Error())
	}
	return errors.New("[" + strings.Join(msgs, ", ") + "]")
}

// formatBytes converts an amount of bytes in a human-readable representation
// the decimal parameter specifies if using powers of 1000 (decimal) or powers of 1024 (binary)
func formatBytes(b uint64, decimal bool) string {
	unit := uint64(1024)
	format := "%.1f %ciB"
	if decimal {
		unit = uint64(1000)
		format = "%.1f %cB"
	}
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf(format, float64(b)/float64(div), "kMGTPE"[exp])
}

// buffer takes an io.Writer and returns a wrapped version of the
// writer that writes to both the original target as well as the returned buffer
func buffer(w io.Writer) (io.Writer, *bytes.Buffer) {
	buffering := &bufferingWriter{buf: bytes.Buffer{}, writer: w}
	return buffering, &buffering.buf
}

type bufferingWriter struct {
	buf    bytes.Buffer
	writer io.Writer
}

func (b *bufferingWriter) Write(p []byte) (n int, err error) {
	if n, err := b.buf.Write(p); err != nil {
		return n, fmt.Errorf("bufferingWriter: error writing to buffer: %w", err)
	}
	return b.writer.Write(p)
}

// hook contains a queued action that can be trigger them when the script
// reaches a certain point (e.g. unsuccessful backup)
type hook struct {
	level  hookLevel
	action func(err error) error
}

type hookLevel int

const (
	hookLevelPlumbing hookLevel = iota
	hookLevelError
	hookLevelInfo
)

var hookLevels = map[string]hookLevel{
	"info":  hookLevelInfo,
	"error": hookLevelError,
}
