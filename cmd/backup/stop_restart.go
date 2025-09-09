// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/docker/cli/cli/command/service/progress"
	ctr "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	"github.com/offen/docker-volume-backup/internal/errwrap"
)

func scaleService(cli *client.Client, serviceID string, replicas uint64) ([]string, error) {
	service, _, err := cli.ServiceInspectWithRaw(context.Background(), serviceID, swarm.ServiceInspectOptions{})
	if err != nil {
		return nil, errwrap.Wrap(err, fmt.Sprintf("error inspecting service %s", serviceID))
	}
	serviceMode := &service.Spec.Mode
	switch {
	case serviceMode.Replicated != nil:
		serviceMode.Replicated.Replicas = &replicas
	default:
		return nil, errwrap.Wrap(nil, fmt.Sprintf("service to be scaled %s has to be in replicated mode", service.Spec.Name))
	}

	response, err := cli.ServiceUpdate(context.Background(), service.ID, service.Version, service.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return nil, errwrap.Wrap(err, "error updating service")
	}

	discardWriter := &noopWriteCloser{io.Discard}
	if err := progress.ServiceProgress(context.Background(), cli, service.ID, discardWriter); err != nil {
		return nil, err
	}
	return response.Warnings, nil
}

func awaitContainerCountForService(cli *client.Client, serviceID string, count int, timeoutAfter time.Duration) error {
	poll := time.NewTicker(time.Second)
	timeout := time.NewTimer(timeoutAfter)
	defer timeout.Stop()
	defer poll.Stop()

	for {
		select {
		case <-timeout.C:
			return errwrap.Wrap(
				nil,
				fmt.Sprintf(
					"timed out after waiting %s for service %s to reach desired container count of %d",
					timeoutAfter,
					serviceID,
					count,
				),
			)
		case <-poll.C:
			containers, err := cli.ContainerList(context.Background(), ctr.ListOptions{
				Filters: filters.NewArgs(filters.KeyValuePair{
					Key:   "label",
					Value: fmt.Sprintf("com.docker.swarm.service.id=%s", serviceID),
				}),
			})
			if err != nil {
				return errwrap.Wrap(err, "error listing containers")
			}
			if len(containers) == count {
				return nil
			}
		}
	}
}

func isSwarm(c interface {
	Info(context.Context) (system.Info, error)
}) (bool, error) {
	info, err := c.Info(context.Background())
	if err != nil {
		return false, errwrap.Wrap(err, "error getting docker info")
	}
	return info.Swarm.LocalNodeState != "" && info.Swarm.LocalNodeState != swarm.LocalNodeStateInactive && info.Swarm.ControlAvailable, nil
}

// stopContainersAndServices stops all Docker containers that are marked as to being
// stopped during the backup and returns a function that can be called to
// restart everything that has been stopped.
func (s *script) stopContainersAndServices() (func() error, error) {
	if s.cli == nil {
		return noop, nil
	}

	isDockerSwarm, err := isSwarm(s.cli)
	if err != nil {
		return noop, errwrap.Wrap(err, "error determining swarm state")
	}

	labelValue := s.c.BackupStopDuringBackupLabel
	if s.c.BackupStopContainerLabel != "" {
		s.logger.Warn(
			"Using BACKUP_STOP_CONTAINER_LABEL has been deprecated and will be removed in the next major version.",
		)
		s.logger.Warn(
			"Please use BACKUP_STOP_DURING_BACKUP_LABEL instead. Refer to the docs for an upgrade guide.",
		)
		if _, ok := os.LookupEnv("BACKUP_STOP_DURING_BACKUP_LABEL"); ok {
			return noop, errwrap.Wrap(nil, "both BACKUP_STOP_DURING_BACKUP_LABEL and BACKUP_STOP_CONTAINER_LABEL have been set, cannot continue")
		}
		labelValue = s.c.BackupStopContainerLabel
	}

	filterMatchLabel := fmt.Sprintf(
		"docker-volume-backup.stop-during-backup=%s",
		labelValue,
	)

	allContainers, err := s.cli.ContainerList(context.Background(), ctr.ListOptions{})
	if err != nil {
		return noop, errwrap.Wrap(err, "error querying for containers")
	}
	containersToStop, err := s.cli.ContainerList(context.Background(), ctr.ListOptions{
		Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: filterMatchLabel,
		}),
	})
	if err != nil {
		return noop, errwrap.Wrap(err, "error querying for containers to stop")
	}

	var allServices []swarm.Service
	var servicesToScaleDown []handledSwarmService
	if isDockerSwarm {
		allServices, err = s.cli.ServiceList(context.Background(), swarm.ServiceListOptions{})
		if err != nil {
			return noop, errwrap.Wrap(err, "error querying for services")
		}
		matchingServices, err := s.cli.ServiceList(context.Background(), swarm.ServiceListOptions{
			Filters: filters.NewArgs(filters.KeyValuePair{
				Key:   "label",
				Value: filterMatchLabel,
			}),
			Status: true,
		})
		if err != nil {
			return noop, errwrap.Wrap(err, "error querying for services to scale down")
		}
		for _, s := range matchingServices {
			if s.Spec.Mode.Replicated == nil {
				return noop, errwrap.Wrap(
					nil,
					fmt.Sprintf("only replicated services can be restarted, but found a label on service %s", s.Spec.Name),
				)
			}
			servicesToScaleDown = append(servicesToScaleDown, handledSwarmService{
				serviceID:           s.ID,
				initialReplicaCount: *s.Spec.Mode.Replicated.Replicas,
			})
		}
	}

	if len(containersToStop) == 0 && len(servicesToScaleDown) == 0 {
		return noop, nil
	}

	if isDockerSwarm {
		for _, container := range containersToStop {
			if swarmServiceID, ok := container.Labels["com.docker.swarm.service.id"]; ok {
				parentService, _, err := s.cli.ServiceInspectWithRaw(context.Background(), swarmServiceID, swarm.ServiceInspectOptions{})
				if err != nil {
					return noop, errwrap.Wrap(err, fmt.Sprintf("error querying for parent service with ID %s", swarmServiceID))
				}
				for label := range parentService.Spec.Labels {
					if label == "docker-volume-backup.stop-during-backup" {
						return noop, errwrap.Wrap(
							nil,
							fmt.Sprintf(
								"container %s is labeled to stop but has parent service %s which is also labeled, cannot continue",
								container.Names[0],
								parentService.Spec.Name,
							),
						)
					}
				}
			}
		}
	}

	s.logger.Info(
		fmt.Sprintf(
			"Stopping %d out of %d running container(s) as they were labeled %s.",
			len(containersToStop),
			len(allContainers),
			filterMatchLabel,
		),
	)
	if isDockerSwarm {
		s.logger.Info(
			fmt.Sprintf(
				"Scaling down %d out of %d active service(s) as they were labeled %s.",
				len(servicesToScaleDown),
				len(allServices),
				filterMatchLabel,
			),
		)
	}

	var stoppedContainers []ctr.Summary
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
					return
				}
				scaledDownServices = append(scaledDownServices, svc)
				for _, warning := range warnings {
					s.logger.Warn(
						fmt.Sprintf("The Docker API returned a warning when scaling down service %s: %s", svc.serviceID, warning),
					)
				}
				// progress.ServiceProgress returns too early, so we need to manually check
				// whether all containers belonging to the service have actually been removed
				if err := awaitContainerCountForService(s.cli, svc.serviceID, 0, s.c.BackupStopServiceTimeout); err != nil {
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
		initialErr = errwrap.Wrap(
			errors.Join(allErrors...),
			fmt.Sprintf(
				"%d error(s) stopping containers",
				len(allErrors),
			),
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
				service, _, err := s.cli.ServiceInspectWithRaw(context.Background(), swarmServiceID, swarm.ServiceInspectOptions{})
				if err != nil {
					restartErrors = append(
						restartErrors,
						errwrap.Wrap(err, "error looking up parent service"),
					)
					continue
				}
				service.Spec.TaskTemplate.ForceUpdate += 1
				if _, err := s.cli.ServiceUpdate(
					context.Background(), service.ID,
					service.Version, service.Spec, swarm.ServiceUpdateOptions{},
				); err != nil {
					restartErrors = append(restartErrors, err)
				}
				continue
			}

			if err := s.cli.ContainerStart(context.Background(), container.ID, ctr.StartOptions{}); err != nil {
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
			return errwrap.Wrap(
				errors.Join(allErrors...),
				fmt.Sprintf(
					"%d error(s) restarting containers and services",
					len(allErrors),
				),
			)
		}

		s.logger.Info(
			fmt.Sprintf(
				"Restarted %d container(s).",
				len(stoppedContainers),
			),
		)
		if isDockerSwarm {
			s.logger.Info(
				fmt.Sprintf(
					"Scaled %d service(s) back up.",
					len(scaledDownServices),
				),
			)
		}

		return nil
	}, initialErr
}
