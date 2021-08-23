// Copyright 2021 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/joho/godotenv"
	"github.com/leekchan/timeutil"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/sirupsen/logrus"
	"github.com/walle/targz"
	"golang.org/x/crypto/openpgp"
)

func main() {
	unlock := lock("/var/dockervolumebackup.lock")
	defer unlock()

	s := &script{}
	s.must(s.init())
	s.must(s.stopContainersAndRun(s.takeBackup))
	s.must(s.encryptBackup())
	s.must(s.copyBackup())
	s.must(s.cleanBackup())
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

	file           string
	bucket         string
	archive        string
	sources        string
	passphrase     []byte
	retentionDays  *int
	leeway         *time.Duration
	containerLabel string
	pruningPrefix  string
}

// init creates all resources needed for the script to perform actions against
// remote resources like the Docker engine or remote storage locations. All
// reading from env vars or other configuration sources is expected to happen
// in this method.
func (s *script) init() error {
	s.ctx = context.Background()
	s.logger = logrus.New()
	s.logger.SetOutput(os.Stdout)

	if err := godotenv.Load("/etc/backup.env"); err != nil {
		return fmt.Errorf("init: failed to load env file: %w", err)
	}

	_, err := os.Stat("/var/run/docker.sock")
	if !os.IsNotExist(err) {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("init: failed to create docker client")
		}
		s.cli = cli
	}

	if bucket := os.Getenv("AWS_S3_BUCKET_NAME"); bucket != "" {
		s.bucket = bucket
		mc, err := minio.New(os.Getenv("AWS_ENDPOINT"), &minio.Options{
			Creds: credentials.NewStaticV4(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				"",
			),
			Secure: os.Getenv("AWS_ENDPOINT_INSECURE") == "" && os.Getenv("AWS_ENDPOINT_PROTO") == "https",
		})
		if err != nil {
			return fmt.Errorf("init: error setting up minio client: %w", err)
		}
		s.mc = mc
	}

	file := os.Getenv("BACKUP_FILENAME")
	if file == "" {
		return errors.New("init: BACKUP_FILENAME not given")
	}
	s.file = path.Join("/tmp", file)
	s.archive = os.Getenv("BACKUP_ARCHIVE")
	s.sources = os.Getenv("BACKUP_SOURCES")
	if v := os.Getenv("GPG_PASSPHRASE"); v != "" {
		s.passphrase = []byte(v)
	}
	if v := os.Getenv("BACKUP_RETENTION_DAYS"); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("init: error parsing BACKUP_RETENTION_DAYS as int: %w", err)
		}
		s.retentionDays = &i
	}
	if v := os.Getenv("BACKUP_PRUNING_LEEWAY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("init: error parsing BACKUP_PRUNING_LEEWAY as duration: %w", err)
		}
		s.leeway = &d
	}
	s.containerLabel = os.Getenv("BACKUP_STOP_CONTAINER_LABEL")
	s.pruningPrefix = os.Getenv("BACKUP_PRUNING_PREFIX")
	s.start = time.Now()

	return nil
}

// stopContainersAndRun stops all Docker containers that are marked as to being
// stopped during the backup and runs the given thunk. After returning, it makes
// sure containers are being restarted if required.
func (s *script) stopContainersAndRun(thunk func() error) error {
	if s.cli == nil {
		return thunk()
	}

	allContainers, err := s.cli.ContainerList(s.ctx, types.ContainerListOptions{
		Quiet: true,
	})
	if err != nil {
		return fmt.Errorf("stopContainersAndRun: error querying for containers: %w", err)
	}

	containerLabel := fmt.Sprintf(
		"docker-volume-backup.stop-during-backup=%s",
		s.containerLabel,
	)
	containersToStop, err := s.cli.ContainerList(s.ctx, types.ContainerListOptions{
		Quiet: true,
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: containerLabel,
		}),
	})

	if err != nil {
		return fmt.Errorf("stopContainersAndRun: error querying for containers to stop: %w", err)
	}

	if len(containersToStop) == 0 {
		return thunk()
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

	defer func() error {
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
		s.logger.Infof("Restarted %d container(s) and the matching service(s).", len(stoppedContainers))
		return nil
	}()

	if len(stopErrors) != 0 {
		return fmt.Errorf(
			"stopContainersAndRun: %d error(s) stopping containers: %w",
			len(stopErrors),
			err,
		)
	}

	return thunk()
}

// takeBackup creates a tar archive of the configured backup location and
// saves it to disk.
func (s *script) takeBackup() error {
	s.file = timeutil.Strftime(&s.start, s.file)
	if err := targz.Compress(s.sources, s.file); err != nil {
		return fmt.Errorf("takeBackup: error compressing backup folder: %w", err)
	}
	s.logger.Infof("Created backup of `%s` at `%s`.", s.sources, s.file)
	return nil
}

// encryptBackup encrypts the backup file using PGP and the configured passphrase.
// In case no passphrase is given it returns early, leaving the backup file
//  untouched.
func (s *script) encryptBackup() error {
	if s.passphrase == nil {
		return nil
	}

	output := bytes.NewBuffer(nil)
	_, name := path.Split(s.file)

	pt, err := openpgp.SymmetricallyEncrypt(output, []byte(s.passphrase), &openpgp.FileHints{
		IsBinary: true,
		FileName: name,
	}, nil)
	if err != nil {
		return fmt.Errorf("encryptBackup: error encrypting backup file: %w", err)
	}

	file, err := os.Open(s.file)
	if err != nil {
		return fmt.Errorf("encryptBackup: error opening unencrypted backup file: %w", err)
	}

	// backup files might be very large, so they are being read in chunks instead
	// of loading them into memory once.
	scanner := bufio.NewScanner(file)
	chunk := make([]byte, 0, 1024*1024)
	scanner.Buffer(chunk, 10*1024*1024)
	for scanner.Scan() {
		_, err = pt.Write(scanner.Bytes())
		if err != nil {
			file.Close()
			pt.Close()
			return fmt.Errorf("encryptBackup: error encrypting backup contents: %w", err)
		}
	}

	file.Close()
	pt.Close()

	gpgFile := fmt.Sprintf("%s.gpg", s.file)
	if err := ioutil.WriteFile(gpgFile, output.Bytes(), os.ModeAppend); err != nil {
		return fmt.Errorf("encryptBackup: error writing encrypted version of backup: %w", err)
	}

	if err := os.Remove(s.file); err != nil {
		return fmt.Errorf("encryptBackup: error removing unencrpyted backup: %w", err)
	}

	s.file = gpgFile
	s.logger.Infof("Encrypted backup using given passphrase, saving as `%s`.", s.file)
	return nil
}

// copyBackup makes sure the backup file is copied to both local and remote locations
// as per the given configuration.
func (s *script) copyBackup() error {
	_, name := path.Split(s.file)
	if s.bucket != "" {
		_, err := s.mc.FPutObject(s.ctx, s.bucket, name, s.file, minio.PutObjectOptions{
			ContentType: "application/tar+gzip",
		})
		if err != nil {
			return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
		}
		s.logger.Infof("Uploaded a copy of backup `%s` to bucket `%s`", s.file, s.bucket)
	}

	if _, err := os.Stat(s.archive); !os.IsNotExist(err) {
		if err := copy(s.file, path.Join(s.archive, name)); err != nil {
			return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
		}
		s.logger.Infof("Stored copy of backup `%s` in local archive `%s`", s.file, s.archive)
	}
	return nil
}

// cleanBackup removes the backup file from disk.
func (s *script) cleanBackup() error {
	if err := os.Remove(s.file); err != nil {
		return fmt.Errorf("cleanBackup: error removing file: %w", err)
	}
	s.logger.Info("Cleaned up local artifacts.")
	return nil
}

// pruneOldBackups rotates away backups from local and remote storages using
// the given configuration. In case the given configuration would delete all
// backups, it does nothing instead.
func (s *script) pruneOldBackups() error {
	if s.retentionDays == nil {
		return nil
	}

	if s.leeway != nil {
		s.logger.Infof("Sleeping for %s before pruning backups.", s.leeway)
		time.Sleep(*s.leeway)
	}

	s.logger.Infof("Trying to prune backups older than %d day(s) now.", *s.retentionDays)
	deadline := s.start.AddDate(0, 0, -*s.retentionDays)

	if s.bucket != "" {
		candidates := s.mc.ListObjects(s.ctx, s.bucket, minio.ListObjectsOptions{
			WithMetadata: true,
			Prefix:       s.pruningPrefix,
		})

		var matches []minio.ObjectInfo
		var lenCandidates int
		for candidate := range candidates {
			lenCandidates++
			if candidate.Err != nil {
				return fmt.Errorf("pruneOldBackups: error looking up candidates from remote storage: %w", candidate.Err)
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
			errChan := s.mc.RemoveObjects(s.ctx, s.bucket, objectsCh, minio.RemoveObjectsOptions{})
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
				"Pruned %d out of %d remote backup(s) as their age exceeded the configured retention period.",
				len(matches),
				lenCandidates,
			)
		} else if len(matches) != 0 && len(matches) == lenCandidates {
			s.logger.Warnf(
				"The current configuration would delete all %d remote backup copies. Refusing to do so, please check your configuration.",
				len(matches),
			)
		} else {
			s.logger.Infof("None of %d remote backup(s) were pruned.", lenCandidates)
		}
	}

	if _, err := os.Stat(s.archive); !os.IsNotExist(err) {
		candidates, err := filepath.Glob(
			path.Join(s.archive, fmt.Sprintf("%s*", s.pruningPrefix)),
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
				"Pruned %d out of %d local backup(s) as their age exceeded the configured retention period.",
				len(matches),
				len(candidates),
			)
		} else if len(matches) != 0 && len(matches) == len(candidates) {
			s.logger.Warnf(
				"The current configuration would delete all %d local backup copies. Refusing to do so, please check your configuration.",
				len(matches),
			)
		} else {
			s.logger.Infof("None of %d local backup(s) were pruned.", len(candidates))
		}
	}
	return nil
}

func (s *script) must(err error) {
	if err != nil {
		if s.logger == nil {
			panic(err)
		}
		s.logger.Errorf("Fatal error running backup: %s", err)
		os.Exit(1)
	}
}

// lock opens a lockfile at the given location, keeping it locked until the
// caller invokes the returned release func. When invoked while the file is
// still locked the function panics.
func lock(lockfile string) func() error {
	lf, err := os.OpenFile(lockfile, os.O_CREATE|os.O_RDWR, os.ModeAppend)
	if err != nil {
		panic(err)
	}
	return func() error {
		if err := lf.Close(); err != nil {
			return fmt.Errorf("lock: error releasing file lock: %w", err)
		}
		if err := os.Remove(lockfile); err != nil {
			return fmt.Errorf("lock: error removing lock file: %w", err)
		}
		return nil
	}
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
