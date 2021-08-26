// Copyright 2021 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/gofrs/flock"
	"github.com/kelseyhightower/envconfig"
	"github.com/leekchan/timeutil"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/sirupsen/logrus"
	"github.com/walle/targz"
	"golang.org/x/crypto/openpgp"
)

func main() {
	unlock := lock("/var/lock/dockervolumebackup.lock")
	defer unlock()

	s, err := newScript()
	if err != nil {
		panic(err)
	}

	s.must(func() error {
		restartContainers, err := s.stopContainers()
		defer s.must(restartContainers())
		if err != nil {
			return err
		}
		return s.takeBackup()
	}())

	s.must(s.encryptBackup())
	s.must(s.copyBackup())
	s.must(s.removeArtifacts())
	s.must(s.pruneOldBackups())
	s.logger.Info("Finished running backup tasks.")
}

// script holds all the stateful information required to orchestrate a
// single backup run.
type script struct {
	ctx    context.Context
	cli    *client.Client
	mc     *minio.Client
	logger *logrus.Logger

	start time.Time
	file  string

	c *config
}

type config struct {
	BackupSources            string        `split_words:"true" default:"/backup"`
	BackupFilename           string        `split_words:"true" default:"backup-%Y-%m-%dT%H-%M-%S.tar.gz"`
	BackupArchive            string        `split_words:"true" default:"/archive"`
	BackupRetentionDays      int32         `split_words:"true" default:"-1"`
	BackupPruningLeeway      time.Duration `split_words:"true" default:"1m"`
	BackupPruningPrefix      string        `split_words:"true"`
	BackupStopContainerLabel string        `split_words:"true" default:"true"`
	AwsS3BucketName          string        `split_words:"true"`
	AwsEndpoint              string        `split_words:"true" default:"s3.amazonaws.com"`
	AwsEndpointProto         string        `split_words:"true" default:"https"`
	AwsEndpointInsecure      bool          `split_words:"true"`
	AwsAccessKeyID           string        `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey       string        `split_words:"true"`
	GpgPassphrase            string        `split_words:"true"`
}

// newScript creates all resources needed for the script to perform actions against
// remote resources like the Docker engine or remote storage locations. All
// reading from env vars or other configuration sources is expected to happen
// in this method.
func newScript() (*script, error) {
	s := &script{
		c:   &config{},
		ctx: context.Background(),
		logger: &logrus.Logger{
			Out:       os.Stdout,
			Formatter: new(logrus.TextFormatter),
			Hooks:     make(logrus.LevelHooks),
			Level:     logrus.InfoLevel,
		},
		start: time.Now(),
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
		mc, err := minio.New(s.c.AwsEndpoint, &minio.Options{
			Creds: credentials.NewStaticV4(
				s.c.AwsAccessKeyID,
				s.c.AwsSecretAccessKey,
				"",
			),
			Secure: !s.c.AwsEndpointInsecure && s.c.AwsEndpointProto == "https",
		})
		if err != nil {
			return nil, fmt.Errorf("newScript: error setting up minio client: %w", err)
		}
		s.mc = mc
	}

	return s, nil
}

var noop = func() error { return nil }

// stopContainers stops all Docker containers that are marked as to being
// stopped during the backup and returns a function that can be called to
// restart everything that has been stopped.
func (s *script) stopContainers() (func() error, error) {
	if s.cli == nil {
		return noop, nil
	}

	allContainers, err := s.cli.ContainerList(s.ctx, types.ContainerListOptions{
		Quiet: true,
	})
	if err != nil {
		return noop, fmt.Errorf("stopContainersAndRun: error querying for containers: %w", err)
	}

	containerLabel := fmt.Sprintf(
		"docker-volume-backup.stop-during-backup=%s",
		s.c.BackupStopContainerLabel,
	)
	containersToStop, err := s.cli.ContainerList(s.ctx, types.ContainerListOptions{
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
		if err := s.cli.ContainerStop(s.ctx, container.ID, nil); err != nil {
			stopErrors = append(stopErrors, err)
		} else {
			stoppedContainers = append(stoppedContainers, container)
		}
	}

	if len(stopErrors) != 0 {
		return noop, fmt.Errorf(
			"stopContainersAndRun: %d error(s) stopping containers: %w",
			len(stopErrors),
			err,
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
			if err := s.cli.ContainerStart(s.ctx, container.ID, types.ContainerStartOptions{}); err != nil {
				restartErrors = append(restartErrors, err)
			}
		}

		if len(servicesRequiringUpdate) != 0 {
			services, _ := s.cli.ServiceList(s.ctx, types.ServiceListOptions{})
			for serviceName := range servicesRequiringUpdate {
				var serviceMatch swarm.Service
				for _, service := range services {
					if service.Spec.Name == serviceName {
						serviceMatch = service
						break
					}
				}
				if serviceMatch.ID == "" {
					return fmt.Errorf("stopContainersAndRun: Couldn't find service with name %s", serviceName)
				}
				serviceMatch.Spec.TaskTemplate.ForceUpdate = 1
				_, err := s.cli.ServiceUpdate(
					s.ctx, serviceMatch.ID,
					serviceMatch.Version, serviceMatch.Spec, types.ServiceUpdateOptions{},
				)
				if err != nil {
					restartErrors = append(restartErrors, err)
				}
			}
		}

		if len(restartErrors) != 0 {
			return fmt.Errorf(
				"stopContainersAndRun: %d error(s) restarting containers and services: %w",
				len(restartErrors),
				err,
			)
		}
		s.logger.Infof(
			"Restarted %d container(s) and the matching service(s).",
			len(stoppedContainers),
		)
		return nil
	}, nil
}

// takeBackup creates a tar archive of the configured backup location and
// saves it to disk.
func (s *script) takeBackup() error {
	s.file = timeutil.Strftime(&s.start, s.file)
	if err := targz.Compress(s.c.BackupSources, s.file); err != nil {
		return fmt.Errorf("takeBackup: error compressing backup folder: %w", err)
	}
	s.logger.Infof("Created backup of `%s` at `%s`.", s.c.BackupSources, s.file)
	return nil
}

// encryptBackup encrypts the backup file using PGP and the configured passphrase.
// In case no passphrase is given it returns early, leaving the backup file
//  untouched.
func (s *script) encryptBackup() error {
	if s.c.GpgPassphrase == "" {
		return nil
	}
	defer os.Remove(s.file)

	gpgFile := fmt.Sprintf("%s.gpg", s.file)
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
		return fmt.Errorf("encryptBackup: error opening backup file %s: %w", s.file, err)
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
	if s.c.AwsS3BucketName != "" {
		_, err := s.mc.FPutObject(s.ctx, s.c.AwsS3BucketName, name, s.file, minio.PutObjectOptions{
			ContentType: "application/tar+gzip",
		})
		if err != nil {
			return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
		}
		s.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`", s.file, s.c.AwsS3BucketName)
	}

	if _, err := os.Stat(s.c.BackupArchive); !os.IsNotExist(err) {
		if err := copy(s.file, path.Join(s.c.BackupArchive, name)); err != nil {
			return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
		}
		s.logger.Infof("Stored copy of backup `%s` in local archive `%s`", s.file, s.c.AwsS3BucketName)
	}
	return nil
}

// removeArtifacts removes the backup file from disk.
func (s *script) removeArtifacts() error {
	if err := os.Remove(s.file); err != nil {
		return fmt.Errorf("removeArtifacts: error removing file: %w", err)
	}
	s.logger.Info("Removed local artifacts.")
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

	s.logger.Infof("Trying to prune backups older than %d day(s) now.", s.c.BackupRetentionDays)
	deadline := time.Now().AddDate(0, 0, -int(s.c.BackupRetentionDays))

	if s.c.AwsS3BucketName != "" {
		candidates := s.mc.ListObjects(s.ctx, s.c.AwsS3BucketName, minio.ListObjectsOptions{
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
			errChan := s.mc.RemoveObjects(s.ctx, s.c.AwsS3BucketName, objectsCh, minio.RemoveObjectsOptions{})
			var errors []error
			for result := range errChan {
				if result.Err != nil {
					errors = append(errors, result.Err)
				}
			}

			if len(errors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d error(s) removing files from remote storage: %w",
					len(errors),
					errors[0],
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
		candidates, err := filepath.Glob(
			path.Join(s.c.BackupArchive, fmt.Sprintf("%s*", s.c.BackupPruningPrefix)),
		)
		if err != nil {
			return fmt.Errorf(
				"pruneOldBackups: error looking up matching files, starting with: %w", err,
			)
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
			var errors []error
			for _, candidate := range matches {
				if err := os.Remove(candidate); err != nil {
					errors = append(errors, err)
				}
			}
			if len(errors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d error(s) deleting local files, starting with: %w",
					len(errors),
					errors[0],
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

func (s *script) must(err error) {
	if err != nil {
		s.logger.Fatalf("Fatal error running backup: %s", err)
	}
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
func copy(src, dst string) error {
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
