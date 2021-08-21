package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/joho/godotenv"
)

func main() {
	s := &script{}
	defer func() {
		for _, thunk := range s.cleanupTasks {
			thunk()
		}
	}()

	s.lock()
	defer s.unlock()

	if err := s.init(); err != nil {
		panic(err)
	}

	if err := s.stopContainers(); err != nil {
		panic(err)
	}

	if err := s.takeBackup(); err != nil {
		panic(err)
	}

	if err := s.restartContainers(); err != nil {
		panic(err)
	}

	if err := s.encryptBackup(); err != nil {
		panic(err)
	}

	if err := s.copyBackup(); err != nil {
		panic(err)
	}

	if err := s.cleanBackup(); err != nil {
		panic(err)
	}

	if err := s.pruneBackups(); err != nil {
		panic(err)
	}
}

type script struct {
	ctx               context.Context
	cli               *client.Client
	stoppedContainers []types.Container
	file              string
	cleanupTasks      []func()
}

func (s *script) lock()   {}
func (s *script) unlock() {}

func (s *script) init() error {
	s.ctx = context.Background()
	if timeout, ok := os.LookupEnv("BACKUP_TIMEOUT_DURATION"); ok {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return fmt.Errorf("init: error parsing given timeout duration: %w", err)
		}
		withTimeout, cancelFunc := context.WithTimeout(context.Background(), d)
		s.ctx = withTimeout
		s.cleanupTasks = append(s.cleanupTasks, cancelFunc)
	}

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

	s.stoppedContainers = stoppedContainers
	if err != nil {
		return fmt.Errorf("stopContainers: error querying for containers to stop: %w", err)
	}
	fmt.Printf("Stopping %d containers\n", len(s.stoppedContainers))

	if len(s.stoppedContainers) != 0 {
		fmt.Println("Stopping containers")
		for _, container := range s.stoppedContainers {
			if err := s.cli.ContainerStop(s.ctx, container.ID, nil); err != nil {
				return fmt.Errorf("stopContainers: error stopping container %s: %w", container.Names[0], err)
			}
		}
	}
	return nil
}

func (s *script) takeBackup() error {
	return errors.New("takeBackup: not implemented yet")
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
	_, ok := os.LookupEnv("GPG_PASSPHRASE")
	if !ok {
		return nil
	}
	return errors.New("encryptBackup: not implemented yet")
}

func (s *script) copyBackup() error {
	return errors.New("copyBackup: not implemented yet")
}

func (s *script) cleanBackup() error {
	return errors.New("cleanBackup: not implemented yet")
}

func (s *script) pruneBackups() error {
	_, ok := os.LookupEnv("BACKUP_RETENTION_DAYS")
	if !ok {
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
