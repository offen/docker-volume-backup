package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/joho/godotenv"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/walle/targz"
	"golang.org/x/crypto/openpgp"
)

func main() {
	s := &script{}

	must(s.lock)()
	defer s.unlock()

	must(s.init)()
	fmt.Println("Successfully initialized resources.")
	must(s.stopContainers)()
	fmt.Println("Successfully stopped containers.")
	must(s.takeBackup)()
	fmt.Println("Successfully took backup.")
	must(s.restartContainers)()
	fmt.Println("Successfully restarted containers.")
	must(s.encryptBackup)()
	fmt.Println("Successfully encrypted backup.")
	must(s.copyBackup)()
	fmt.Println("Successfully copied backup.")
	must(s.cleanBackup)()
	fmt.Println("Successfully cleaned local backup.")
	must(s.pruneOldBackups)()
	fmt.Println("Successfully pruned old backup.")
}

type script struct {
	ctx               context.Context
	cli               *client.Client
	mc                *minio.Client
	stoppedContainers []types.Container
	releaseLock       func() error
	file              string
}

func (s *script) lock() error {
	lf, err := os.OpenFile("/var/dockervolumebackup.lock", os.O_CREATE, os.ModeAppend)
	if err != nil {
		return fmt.Errorf("lock: error opening lock file: %w", err)
	}
	s.releaseLock = lf.Close
	return nil
}

func (s *script) unlock() error {
	if err := s.releaseLock(); err != nil {
		return fmt.Errorf("unlock: error releasing file lock: %w", err)
	}
	if err := os.Remove("/var/dockervolumebackup.lock"); err != nil {
		return fmt.Errorf("unlock: error removing lock file: %w", err)
	}
	return nil
}

func (s *script) init() error {
	s.ctx = context.Background()

	if err := godotenv.Load("/etc/backup.env"); err != nil {
		return fmt.Errorf("init: failed to load env file: %w", err)
	}

	socketExists, err := fileExists("/var/run/docker.sock")
	if err != nil {
		return fmt.Errorf("init: error checking whether docker.sock is available: %w", err)
	}
	if socketExists {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("init: failied to create docker client")
		}
		s.cli = cli
	}

	if bucket := os.Getenv("AWS_S3_BUCKET_NAME"); bucket != "" {
		mc, err := minio.New(os.Getenv("AWS_ENDPOINT"), &minio.Options{
			Creds: credentials.NewStaticV4(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				"",
			),
			Secure: os.Getenv("AWS_ENDPOINT_PROTO") == "https",
		})
		if err != nil {
			return fmt.Errorf("init: error setting up minio client: %w", err)
		}
		s.mc = mc
	}
	return nil
}

func (s *script) stopContainers() error {
	if s.cli == nil {
		return nil
	}
	allContainers, err := s.cli.ContainerList(s.ctx, types.ContainerListOptions{
		Quiet: true,
	})
	if err != nil {
		return fmt.Errorf("stopContainers: error querying for containers: %w", err)
	}

	containersToStop, err := s.cli.ContainerList(s.ctx, types.ContainerListOptions{
		Quiet: true,
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: fmt.Sprintf("docker-volume-backup.stop-during-backup=%s", os.Getenv("BACKUP_STOP_CONTAINER_LABEL")),
		}),
	})

	if err != nil {
		return fmt.Errorf("stopContainers: error querying for containers to stop: %w", err)
	}
	fmt.Printf("Stopping %d out of %d running containers\n", len(containersToStop), len(allContainers))

	if len(containersToStop) != 0 {
		for _, container := range s.stoppedContainers {
			if err := s.cli.ContainerStop(s.ctx, container.ID, nil); err != nil {
				return fmt.Errorf(
					"stopContainers: error stopping container %s: %w",
					container.Names[0],
					err,
				)
			}
		}
	}

	s.stoppedContainers = containersToStop
	return nil
}

func (s *script) takeBackup() error {
	if os.Getenv("BACKUP_FILENAME") == "" {
		return errors.New("takeBackup: BACKUP_FILENAME not given")
	}

	outBytes, err := exec.Command("date", fmt.Sprintf("+%s", os.Getenv("BACKUP_FILENAME"))).Output()
	if err != nil {
		return fmt.Errorf("takeBackup: error formatting filename template: %w", err)
	}
	file := fmt.Sprintf("/tmp/%s", strings.TrimSpace(string(outBytes)))

	s.file = file
	if err := targz.Compress(os.Getenv("BACKUP_SOURCES"), s.file); err != nil {
		return fmt.Errorf("takeBackup: error compressing backup folder: %w", err)
	}
	return nil
}

func (s *script) restartContainers() error {
	servicesRequiringUpdate := map[string]struct{}{}
	for _, container := range s.stoppedContainers {
		if swarmServiceName, ok := container.Labels["com.docker.swarm.service.name"]; ok {
			servicesRequiringUpdate[swarmServiceName] = struct{}{}
			continue
		}
		if err := s.cli.ContainerStart(s.ctx, container.ID, types.ContainerStartOptions{}); err != nil {
			panic(err)
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
				return fmt.Errorf("restartContainers: Couldn't find service with name %s", serviceName)
			}
			serviceMatch.Spec.TaskTemplate.ForceUpdate = 1
			s.cli.ServiceUpdate(
				s.ctx, serviceMatch.ID,
				serviceMatch.Version, serviceMatch.Spec, types.ServiceUpdateOptions{},
			)
		}
	}

	s.stoppedContainers = []types.Container{}
	return nil
}

func (s *script) encryptBackup() error {
	passphrase := os.Getenv("GPG_PASSPHRASE")
	if passphrase == "" {
		return nil
	}

	buf := bytes.NewBuffer(nil)
	_, name := path.Split(s.file)
	pt, err := openpgp.SymmetricallyEncrypt(buf, []byte(passphrase), &openpgp.FileHints{
		IsBinary: true,
		FileName: name,
	}, nil)
	if err != nil {
		return fmt.Errorf("encryptBackup: error encrypting backup file: %w", err)
	}

	unencrypted, err := ioutil.ReadFile(s.file)
	if err != nil {
		pt.Close()
		return fmt.Errorf("encryptBackup: error reading unencrypted backup file: %w", err)
	}
	_, err = pt.Write(unencrypted)
	if err != nil {
		pt.Close()
		return fmt.Errorf("encryptBackup: error writing backup contents: %w", err)
	}
	pt.Close()

	gpgFile := fmt.Sprintf("%s.gpg", s.file)
	if err := ioutil.WriteFile(gpgFile, buf.Bytes(), os.ModeAppend); err != nil {
		return fmt.Errorf("encryptBackup: error writing encrypted version of backup: %w", err)
	}

	if err := os.Remove(s.file); err != nil {
		return fmt.Errorf("encryptBackup: error removing unencrpyted backup: %w", err)
	}
	s.file = gpgFile
	return nil
}

func (s *script) copyBackup() error {
	_, name := path.Split(s.file)
	if bucket := os.Getenv("AWS_S3_BUCKET_NAME"); bucket != "" {
		_, err := s.mc.FPutObject(s.ctx, bucket, name, s.file, minio.PutObjectOptions{
			ContentType: "application/tar+gzip",
		})
		if err != nil {
			return fmt.Errorf("copyBackup: error uploading backup to remote storage: %w", err)
		}
	}

	if archive := os.Getenv("BACKUP_ARCHIVE"); archive != "" {
		if _, err := os.Stat(archive); !os.IsNotExist(err) {
			if err := copy(s.file, path.Join(archive, name)); err != nil {
				return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
			}
		}
	}
	return nil
}

func (s *script) cleanBackup() error {
	if err := os.Remove(s.file); err != nil {
		return fmt.Errorf("cleanBackup: error removing file: %w", err)
	}
	return nil
}

func (s *script) pruneOldBackups() error {
	retention := os.Getenv("BACKUP_RETENTION_DAYS")
	if retention == "" {
		return nil
	}
	retentionDays, err := strconv.Atoi(retention)
	if err != nil {
		return fmt.Errorf("pruneOldBackups: error parsing BACKUP_RETENTION_DAYS as int: %w", err)
	}
	sleepFor, err := time.ParseDuration(os.Getenv("BACKUP_PRUNING_LEEWAY"))
	if err != nil {
		return fmt.Errorf("pruneBackups: error parsing given leeway value: %w", err)
	}
	time.Sleep(sleepFor)

	deadline := time.Now().AddDate(0, 0, -retentionDays)

	if bucket := os.Getenv("AWS_S3_BUCKET_NAME"); bucket != "" {
		candidates := s.mc.ListObjects(s.ctx, bucket, minio.ListObjectsOptions{
			WithMetadata: true,
			Prefix:       os.Getenv("BACKUP_PRUNING_PREFIX"),
		})

		var matches []minio.ObjectInfo
		for candidate := range candidates {
			if candidate.LastModified.Before(deadline) {
				matches = append(matches, candidate)
			}
		}

		if len(matches) != len(candidates) {
			objectsCh := make(chan minio.ObjectInfo)
			go func() {
				for _, candidate := range matches {
					objectsCh <- candidate
				}
			}()
			errChan := s.mc.RemoveObjects(s.ctx, bucket, objectsCh, minio.RemoveObjectsOptions{})
			var errors []error
			for result := range errChan {
				if result.Err != nil {
					errors = append(errors, result.Err)
				}
			}

			if len(errors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d errors removing files from remote storage: %w",
					len(errors),
					errors[0],
				)
			}
		} else if len(candidates) != 0 {
			fmt.Println("Refusing to delete all backups. Check your configuration.")
		}
	}

	if archive := os.Getenv("BACKUP_ARCHIVE"); archive != "" {
		candidates, err := filepath.Glob(
			path.Join(archive, fmt.Sprintf("%s%s", os.Getenv("BACKUP_PRUNING_PREFIX"), "*")),
		)
		if err != nil {
			return fmt.Errorf(
				"pruneOldBackups: error looking up matching files, starting with: %w", err,
			)
		}

		var matches []os.FileInfo
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
				matches = append(matches, fi)
			}
		}

		if len(matches) != len(candidates) {
			var errors []error
			for _, candidate := range matches {
				if err := os.Remove(candidate.Name()); err != nil {
					errors = append(errors, err)
				}
			}
			if len(errors) != 0 {
				return fmt.Errorf(
					"pruneOldBackups: %d errors deleting local files, starting with: %w",
					len(errors),
					errors[0],
				)
			}
		} else if len(candidates) != 0 {
			fmt.Println("Refusing to delete all backups. Check your configuration.")
		}
	}
	return nil
}

func fileExists(location string) (bool, error) {
	_, err := os.Stat(location)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return err == nil, nil
}

func must(f func() error) func() {
	return func() {
		if err := f(); err != nil {
			panic(err)
		}
	}
}

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
