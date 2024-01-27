package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/cli/cli/command/service/progress"
	"github.com/docker/docker/api/types"
	ctr "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

func scaleService(cli *client.Client, serviceID string, replicas uint64) ([]string, error) {
	service, _, err := cli.ServiceInspectWithRaw(context.Background(), serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("scaleService: error inspecting service %s: %w", serviceID, err)
	}
	serviceMode := &service.Spec.Mode
	switch {
	case serviceMode.Replicated != nil:
		serviceMode.Replicated.Replicas = &replicas
	default:
		return nil, fmt.Errorf("scaleService: service to be scaled %s has to be in replicated mode", service.Spec.Name)
	}

	response, err := cli.ServiceUpdate(context.Background(), service.ID, service.Version, service.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("scaleService: error updating service: %w", err)
	}

	discardWriter := &noopWriteCloser{io.Discard}
	if err := progress.ServiceProgress(context.Background(), cli, service.ID, discardWriter); err != nil {
		return nil, err
	}
	return response.Warnings, nil
}

func awaitContainerCountForService(cli *client.Client, serviceID string, count int) error {
	poll := time.NewTicker(time.Second)
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()
	defer poll.Stop()

	for {
		select {
		case <-timeout.C:
			return fmt.Errorf(
				"awaitContainerCount: timed out after waiting 5 minutes for service %s to reach desired container count of %d",
				serviceID,
				count,
			)
		case <-poll.C:
			containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{
				Filters: filters.NewArgs(filters.KeyValuePair{
					Key:   "label",
					Value: fmt.Sprintf("com.docker.swarm.service.id=%s", serviceID),
				}),
			})
			if err != nil {
				return fmt.Errorf("awaitContainerCount: error listing containers: %w", err)
			}
			if len(containers) == count {
				return nil
			}
		}
	}
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

	var scaledDownServices []handledSwarmService
	var scaleDownErrors concurrentSlice[error]
	if isDockerSwarm {
		wg := sync.WaitGroup{}
		for _, svc := range servicesToScaleDown {
			wg.Add(1)
			go func(svc handledSwarmService) {
				defer wg.Done()
				warnings, err := scaleService(s.cli, svc.serviceID, 0)
				if err != nil {
					scaleDownErrors.append(err)
				} else {
					scaledDownServices = append(scaledDownServices, svc)
				}
				for _, warning := range warnings {
					s.logger.Warn(
						fmt.Sprintf("The Docker API returned a warning when scaling down service %s: %s", svc.serviceID, warning),
					)
				}
				// progress.ServiceProgress returns too early, so we need to manually check
				// whether all containers belonging to the service have actually been removed
				if err := awaitContainerCountForService(s.cli, svc.serviceID, 0); err != nil {
					scaleDownErrors.append(err)
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
		var restartErrors []error
		matchedServices := map[string]bool{}
		for _, container := range stoppedContainers {
			if swarmServiceID, ok := container.Labels["com.docker.swarm.service.id"]; ok && isDockerSwarm {
				if _, ok := matchedServices[swarmServiceID]; ok {
					continue
				}
				matchedServices[swarmServiceID] = true
				// in case a container was part of a swarm service, the service requires to
				// be force updated instead of restarting the container as it would otherwise
				// remain in a "completed" state
				service, _, err := s.cli.ServiceInspectWithRaw(context.Background(), swarmServiceID, types.ServiceInspectOptions{})
				if err != nil {
					restartErrors = append(
						restartErrors,
						fmt.Errorf("(*script).stopContainersAndServices: error looking up parent service: %w", err),
					)
					continue
				}
				service.Spec.TaskTemplate.ForceUpdate += 1
				if _, err := s.cli.ServiceUpdate(
					context.Background(), service.ID,
					service.Version, service.Spec, types.ServiceUpdateOptions{},
				); err != nil {
					restartErrors = append(restartErrors, err)
				}
				continue
			}

			if err := s.cli.ContainerStart(context.Background(), container.ID, types.ContainerStartOptions{}); err != nil {
				restartErrors = append(restartErrors, err)
			}
		}

		var scaleUpErrors concurrentSlice[error]
		if isDockerSwarm {
			wg := &sync.WaitGroup{}
			for _, svc := range servicesToScaleDown {
				wg.Add(1)
				go func(svc handledSwarmService) {
					defer wg.Done()
					warnings, err := scaleService(s.cli, svc.serviceID, svc.initialReplicaCount)
					if err != nil {
						scaleDownErrors.append(err)
						return
					}
					for _, warning := range warnings {
						s.logger.Warn(
							fmt.Sprintf("The Docker API returned a warning when scaling up service %s: %s", svc.serviceID, warning),
						)
					}
				}(svc)
			}
			wg.Wait()
		}

		allErrors := append(restartErrors, scaleUpErrors.value()...)
		if len(allErrors) != 0 {
			return fmt.Errorf(
				"(*script).stopContainersAndServices: %d error(s) restarting containers and services: %w",
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
