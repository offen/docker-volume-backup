package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/joho/godotenv"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/walle/targz"
)

func main() {
	s := &script{}

	s.lock()
	defer s.unlock()

	must(s.init)()
	must(s.stopContainers)()
	must(s.takeBackup)()
	must(s.restartContainers)()
	must(s.encryptBackup)()
	must(s.copyBackup)()
	must(s.cleanBackup)()
	must(s.pruneBackups)()
}

type script struct {
	ctx               context.Context
	cli               *client.Client
	mc                *minio.Client
	stoppedContainers []types.Container
	file              string
}

func (s *script) lock()   {}
func (s *script) unlock() {}

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
	stoppedContainers, err := s.cli.ContainerList(s.ctx, types.ContainerListOptions{
		Quiet: true,
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: fmt.Sprintf("docker-volume-backup.stop-during-backup=%s", os.Getenv("BACKUP_STOP_CONTAINER_LABEL")),
		}),
	})

	if err != nil {
		return fmt.Errorf("stopContainers: error querying for containers to stop: %w", err)
	}
	fmt.Printf("Stopping %d containers\n", len(stoppedContainers))

	if len(stoppedContainers) != 0 {
		fmt.Println("Stopping containers")
		for _, container := range s.stoppedContainers {
			if err := s.cli.ContainerStop(s.ctx, container.ID, nil); err != nil {
				return fmt.Errorf("stopContainers: error stopping container %s: %w", container.Names[0], err)
			}
		}
	}

	s.stoppedContainers = stoppedContainers
	return nil
}

func (s *script) takeBackup() error {
	file := os.Getenv("BACKUP_FILENAME")
	if file == "" {
		return errors.New("takeBackup: BACKUP_FILENAME not given")
	}
	s.file = file
	if err := targz.Compress(os.Getenv("BACKUP_SOURCES"), s.file); err != nil {
		return fmt.Errorf("takeBackup: error compressing backup folder: %w", err)
	}
	return nil
}

func (s *script) restartContainers() error {
	fmt.Println("Starting containers/services back up")
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
	return nil
}

func (s *script) encryptBackup() error {
	key := os.Getenv("GPG_PASSPHRASE")
	if key == "" {
		return nil
	}
	return errors.New("encryptBackup: not implemented yet")
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

func (s *script) pruneBackups() error {
	retention := os.Getenv("BACKUP_RETENTION_DAYS")
	if retention == "" {
		return nil
	}
	return errors.New("pruneBackups: not implemented yet")
}

func fileExists(location string) (bool, error) {
	_, err := os.Stat(location)
	if err != nil && err != os.ErrNotExist {
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
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}
