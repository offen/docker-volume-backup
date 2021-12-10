// Copyright 2021 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/go-gomail/gomail"
	"github.com/gofrs/flock"
	"github.com/kelseyhightower/envconfig"
	"github.com/leekchan/timeutil"
	"github.com/m90/targz"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/otiai10/copy"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/openpgp"
)

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
				if hookErr := s.runHooks(err, hookLevelCleanup, hookLevelFailure); hookErr != nil {
					s.logger.Errorf("An error occurred calling the registered hooks: %s", hookErr)
				}
				os.Exit(1)
			}
			panic(pArg)
		}

		if err := s.runHooks(nil, hookLevelCleanup); err != nil {
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
	s.must(s.pruneOldBackups())
}

// script holds all the stateful information required to orchestrate a
// single backup run.
type script struct {
	cli    *client.Client
	mc     *minio.Client
	logger *logrus.Logger
	hooks  []hook

	start  time.Time
	file   string
	output *bytes.Buffer

	c *config
}

type config struct {
	BackupSources              string        `split_words:"true" default:"/backup"`
	BackupFilename             string        `split_words:"true" default:"backup-%Y-%m-%dT%H-%M-%S.tar.gz"`
	BackupLatestSymlink        string        `split_words:"true"`
	BackupArchive              string        `split_words:"true" default:"/archive"`
	BackupRetentionDays        int32         `split_words:"true" default:"-1"`
	BackupPruningLeeway        time.Duration `split_words:"true" default:"1m"`
	BackupPruningPrefix        string        `split_words:"true"`
	BackupStopContainerLabel   string        `split_words:"true" default:"true"`
	BackupFromSnapshot         bool          `split_words:"true"`
	BackupUID                  int           `split_words:"true" default:"-1"`
	BackupGID                  int           `split_words:"true" default:"-1"`
	AwsS3BucketName            string        `split_words:"true"`
	AwsEndpoint                string        `split_words:"true" default:"s3.amazonaws.com"`
	AwsEndpointProto           string        `split_words:"true" default:"https"`
	AwsEndpointInsecure        bool          `split_words:"true"`
	AwsAccessKeyID             string        `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey         string        `split_words:"true"`
	AwsIamRoleEndpoint         string        `split_words:"true"`
	GpgPassphrase              string        `split_words:"true"`
	EmailNotificationRecipient string        `split_words:"true"`
	EmailNotificationSender    string        `split_words:"true" default:"noreply@nohost"`
	EmailSMTPHost              string        `envconfig:"EMAIL_SMTP_HOST"`
	EmailSMTPPort              int           `envconfig:"EMAIL_SMTP_PORT" default:"587"`
	EmailSMTPUsername          string        `envconfig:"EMAIL_SMTP_USERNAME"`
	EmailSMTPPassword          string        `envconfig:"EMAIL_SMTP_PASSWORD"`
}

var msgBackupFailed = "backup run failed"

// newScript creates all resources needed for the script to perform actions against
// remote resources like the Docker engine or remote storage locations. All
// reading from env vars or other configuration sources is expected to happen
// in this method.
func newScript() (*script, error) {
	stdOut, logBuffer := buffer(os.Stdout)
	s := &script{
		c: &config{},
		logger: &logrus.Logger{
			Out:       stdOut,
			Formatter: new(logrus.TextFormatter),
			Hooks:     make(logrus.LevelHooks),
			Level:     logrus.InfoLevel,
		},
		start:  time.Now(),
		output: logBuffer,
	}

	if err := envconfig.Process("", s.c); err != nil {
		return nil, fmt.Errorf("newScript: failed to process configuration values: %w", err)
	}

	s.file = path.Join("/tmp", s.c.BackupFilename)

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
		s.mc = mc
	}

	if s.c.EmailNotificationRecipient != "" {
		s.hooks = append(s.hooks, hook{hookLevelFailure, func(err error, start time.Time, logOutput string) error {
			mailer := gomail.NewDialer(
				s.c.EmailSMTPHost, s.c.EmailSMTPPort, s.c.EmailSMTPUsername, s.c.EmailSMTPPassword,
			)

			subject := fmt.Sprintf(
				"Failure running docker-volume-backup at %s", start.Format(time.RFC3339),
			)
			body := fmt.Sprintf(
				"Running docker-volume-backup failed with error: %s\n\nLog output of the failed run was:\n\n%s\n", err, logOutput,
			)

			message := gomail.NewMessage()
			message.SetHeader("From", s.c.EmailNotificationSender)
			message.SetHeader("To", s.c.EmailNotificationRecipient)
			message.SetHeader("Subject", subject)
			message.SetBody("text/plain", body)
			return mailer.DialAndSend(message)
		}})
	}

	return s, nil
}

var noop = func() error { return nil }

// registerHook adds the given action at the given level.
func (s *script) registerHook(level hookLevel, action func(err error, start time.Time, logOutput string) error) {
	s.hooks = append(s.hooks, hook{level, action})
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
	s.file = timeutil.Strftime(&s.start, s.file)
	backupSources := s.c.BackupSources

	if s.c.BackupFromSnapshot {
		backupSources = filepath.Join("/tmp", s.c.BackupSources)
		// copy before compressing guard against a situation where backup folder's content are still growing.
		s.registerHook(hookLevelCleanup, func(error, time.Time, string) error {
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
	s.registerHook(hookLevelCleanup, func(error, time.Time, string) error {
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
	s.registerHook(hookLevelCleanup, func(error, time.Time, string) error {
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
	if s.mc != nil {
		if _, err := s.mc.FPutObject(context.Background(), s.c.AwsS3BucketName, name, s.file, minio.PutObjectOptions{
			ContentType: "application/tar+gzip",
		}); err != nil {
			return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
		}
		s.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`.", s.file, s.c.AwsS3BucketName)
	}

	if _, err := os.Stat(s.c.BackupArchive); !os.IsNotExist(err) {
		if err := os.Chown(s.file, s.c.BackupUID, s.c.BackupGID); err != nil {
			return fmt.Errorf("copyBackup: error changing owner on temp file: %w", err)
		}
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

	if s.mc != nil {
		candidates := s.mc.ListObjects(context.Background(), s.c.AwsS3BucketName, minio.ListObjectsOptions{
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

		if len(matches) != 0 && len(matches) != lenCandidates {
			objectsCh := make(chan minio.ObjectInfo)
			go func() {
				for _, match := range matches {
					objectsCh <- match
				}
				close(objectsCh)
			}()
			errChan := s.mc.RemoveObjects(context.Background(), s.c.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
			var removeErrors []error
			for result := range errChan {
				if result.Err != nil {
					removeErrors = append(removeErrors, result.Err)
				}
			}

			if len(removeErrors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d error(s) removing files from remote storage: %w",
					len(removeErrors),
					join(removeErrors...),
				)
			}
			s.logger.Infof(
				"Pruned %d out of %d remote backup(s) as their age exceeded the configured retention period of %d days.",
				len(matches),
				lenCandidates,
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

		if len(matches) != 0 && len(matches) != len(candidates) {
			var removeErrors []error
			for _, match := range matches {
				if err := os.Remove(match); err != nil {
					removeErrors = append(removeErrors, err)
				}
			}
			if len(removeErrors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d error(s) deleting local files, starting with: %w",
					len(removeErrors),
					join(removeErrors...),
				)
			}
			s.logger.Infof(
				"Pruned %d out of %d local backup(s) as their age exceeded the configured retention period of %d days.",
				len(matches),
				len(candidates),
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
func (s *script) runHooks(err error, levels ...hookLevel) error {
	var actionErrors []error
	for _, level := range levels {
		for _, hook := range s.hooks {
			if hook.level != level {
				continue
			}
			if actionErr := hook.action(err, s.start, s.output.String()); actionErr != nil {
				actionErrors = append(actionErrors, fmt.Errorf("runHooks: error running hook: %w", actionErr))
			}
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
		return fmt.Errorf("copyFile: error opening source file: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("copyFile: error creating destination: %w", err)
	}

	_, err = io.Copy(out, in)
	if err != nil {
		out.Close()
		return fmt.Errorf("copyFile: error copying: %w", err)
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
	action func(err error, start time.Time, logOutput string) error
}

type hookLevel int

const (
	hookLevelFailure hookLevel = iota
	hookLevelCleanup
)
