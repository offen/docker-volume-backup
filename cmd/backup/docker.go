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
)

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
