// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/cli/cli/command/service/progress"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/offen/docker-volume-backup/internal/errwrap"
)

const STOP_DURING_BACKUP_LABEL = "docker-volume-backup.stop-during-backup"
const STOP_DURING_BACKUP_NO_RESTART_LABEL = "docker-volume-backup.stop-during-backup-no-restart"

func scaleService(cli *client.Client, serviceID string, replicas uint64) ([]string, error) {
	result, err := cli.ServiceInspect(context.Background(), serviceID, client.ServiceInspectOptions{})
	if err != nil {
		return nil, errwrap.Wrap(err, fmt.Sprintf("error inspecting service %s", serviceID))
	}
	service := result.Service
	serviceMode := &service.Spec.Mode
	switch {
	case serviceMode.Replicated != nil:
		serviceMode.Replicated.Replicas = &replicas
	default:
		return nil, errwrap.Wrap(nil, fmt.Sprintf("service to be scaled %s has to be in replicated mode", service.Spec.Name))
	}

	response, err := cli.ServiceUpdate(context.Background(), service.ID, client.ServiceUpdateOptions{Version: service.Version, Spec: service.Spec})
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
			containers, err := cli.ContainerList(context.Background(), client.ContainerListOptions{
				Filters: client.Filters{}.Add("label", fmt.Sprintf("com.docker.swarm.service.id=%s", serviceID)),
			})
			if err != nil {
				return errwrap.Wrap(err, "error listing containers")
			}
			if len(containers.Items) == count {
				return nil
			}
		}
	}
}

func isSwarm(c interface {
	Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error)
}) (bool, error) {
	result, err := c.Info(context.Background(), client.InfoOptions{})
	if err != nil {
		return false, errwrap.Wrap(err, "error getting docker info")
	}
	return result.Info.Swarm.LocalNodeState != "" && result.Info.Swarm.LocalNodeState != swarm.LocalNodeStateInactive && result.Info.Swarm.ControlAvailable, nil
}

func hasLabel(labels map[string]string, key, value string) bool {
	val, ok := labels[key]
	return ok && val == value
}

func checkStopLabels(labels map[string]string, stopDuringBackupLabelValue string, stopDuringBackupNoRestartLabelValue string) (bool, bool, error) {
	hasStopDuringBackupLabel := hasLabel(labels, STOP_DURING_BACKUP_LABEL, stopDuringBackupLabelValue)
	hasStopDuringBackupNoRestartLabel := hasLabel(labels, STOP_DURING_BACKUP_NO_RESTART_LABEL, stopDuringBackupNoRestartLabelValue)
	if hasStopDuringBackupLabel && hasStopDuringBackupNoRestartLabel {
		return hasStopDuringBackupLabel, hasStopDuringBackupNoRestartLabel, errwrap.Wrap(nil, fmt.Sprintf("both %s and %s have been set, cannot continue", STOP_DURING_BACKUP_LABEL, STOP_DURING_BACKUP_NO_RESTART_LABEL))
	}

	return hasStopDuringBackupLabel, hasStopDuringBackupNoRestartLabel, nil
}

// stopContainersAndServices stops all Docker containers that are marked as to being
// stopped during the backup and returns a function that can be called to
// restart everything that has been stopped.
func (s *script) stopContainersAndServices() (func() error, error) {
	if s.cli == nil {
		return noop, nil
	}

	if len(s.containersToStop) == 0 && len(s.servicesToScaleDown) == 0 {
		return noop, nil
	}

	if s.isSwarm {
		for _, container := range s.containersToStop {
			if swarmServiceID, ok := container.summary.Labels["com.docker.swarm.service.id"]; ok {
				parentService, err := s.cli.ServiceInspect(context.Background(), swarmServiceID, client.ServiceInspectOptions{})
				if err != nil {
					return noop, errwrap.Wrap(err, fmt.Sprintf("error querying for parent service with ID %s", swarmServiceID))
				}
				for label := range parentService.Service.Spec.Labels {
					if label == STOP_DURING_BACKUP_LABEL {
						return noop, errwrap.Wrap(
							nil,
							fmt.Sprintf(
								"container %s is labeled to stop but has parent service %s which is also labeled, cannot continue",
								container.summary.Names[0],
								parentService.Service.Spec.Name,
							),
						)
					}
				}
			}
		}
	}

	allContainers, err := s.cli.ContainerList(context.Background(), client.ContainerListOptions{})
	if err != nil {
		return noop, errwrap.Wrap(err, "error querying for containers")
	}
	s.logger.Info(
		fmt.Sprintf(
			"Stopping %d out of %d running container(s) as they were labeled %s=%s or %s=%s.",
			len(s.containersToStop),
			len(allContainers.Items),
			STOP_DURING_BACKUP_LABEL,
			s.c.BackupStopDuringBackupLabel,
			STOP_DURING_BACKUP_NO_RESTART_LABEL,
			s.c.BackupStopDuringBackupNoRestartLabel,
		),
	)

	var allServices []swarm.Service
	if s.isSwarm {
		result, err := s.cli.ServiceList(context.Background(), client.ServiceListOptions{Status: true})
		if err != nil {
			return noop, errwrap.Wrap(err, "error querying for services")
		}
		allServices = result.Items
		s.logger.Info(
			fmt.Sprintf(
				"Scaling down %d out of %d active service(s) as they were labeled %s=%s or %s=%s.",
				len(s.servicesToScaleDown),
				len(allServices),
				STOP_DURING_BACKUP_LABEL,
				s.c.BackupStopDuringBackupLabel,
				STOP_DURING_BACKUP_NO_RESTART_LABEL,
				s.c.BackupStopDuringBackupNoRestartLabel,
			),
		)
	}

	var stoppedContainers []handledContainer
	var stopErrors []error
	for _, container := range s.containersToStop {
		if _, err := s.cli.ContainerStop(context.Background(), container.summary.ID, client.ContainerStopOptions{}); err != nil {
			stopErrors = append(stopErrors, err)
		} else {
			stoppedContainers = append(stoppedContainers, container)
		}
	}

	var scaledDownServices []handledSwarmService
	var scaleDownErrors concurrentSlice[error]
	if s.isSwarm {
		wg := sync.WaitGroup{}
		for _, svc := range s.servicesToScaleDown {
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
		All:        uint(len(allContainers.Items)),
		ToStop:     uint(len(s.containersToStop)),
		Stopped:    uint(len(stoppedContainers)),
		StopErrors: uint(len(stopErrors)),
	}

	s.stats.Services = ServicesStats{
		All:             uint(len(allServices)),
		ToScaleDown:     uint(len(s.servicesToScaleDown)),
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
		var restartedContainers []handledContainer

		containerIDsToStopInFutureRuns, serviceIDsToScaleDownInFutureRuns, err := s.loadContainerIdSAndServiceIDsToStop()
		if err != nil {
			return errwrap.Wrap(err, "error loading waiting files for restart")
		}

		matchedServices := map[string]bool{}
		for _, container := range stoppedContainers {
			if !container.restart {
				continue
			}

			if swarmServiceID, ok := container.summary.Labels["com.docker.swarm.service.id"]; ok && s.isSwarm {
				if _, ok := matchedServices[swarmServiceID]; ok {
					continue
				}
				matchedServices[swarmServiceID] = true
				// in case a container was part of a swarm service, the service requires to
				// be force updated instead of restarting the container as it would otherwise
				// remain in a "completed" state
				result, err := s.cli.ServiceInspect(context.Background(), swarmServiceID, client.ServiceInspectOptions{})
				service := result.Service
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
					client.ServiceUpdateOptions{Spec: service.Spec, Version: service.Version},
				); err != nil {
					restartErrors = append(restartErrors, err)
				}
				continue
			}

			if s.c.ActivateLazyRestart {
				if _, ok := containerIDsToStopInFutureRuns[container.summary.ID]; ok {
					// skip restarting this container as there is another backup run in the future that will stop it again, so restarting it now would be pointless and might cause issues if the container cannot be stopped gracefully a second time
					continue
				}
			}

			if _, err := s.cli.ContainerStart(context.Background(), container.summary.ID, client.ContainerStartOptions{}); err != nil {
				restartErrors = append(restartErrors, err)
			} else {
				restartedContainers = append(restartedContainers, container)
			}
		}

		var scaleUpErrors concurrentSlice[error]
		var scaledUpServices []handledSwarmService
		if s.isSwarm {
			wg := &sync.WaitGroup{}
			for _, svc := range scaledDownServices {
				if !svc.restart {
					continue
				}
				if s.c.ActivateLazyRestart {
					if _, ok := serviceIDsToScaleDownInFutureRuns[svc.serviceID]; ok {
						// skip restarting this service as there is another backup run in the future that will scale it down again, so scaling it up now would be pointless and might cause issues if the service cannot be scaled down gracefully a second time
						continue
					}
				}

				wg.Add(1)
				go func(svc handledSwarmService) {
					defer wg.Done()
					warnings, err := scaleService(s.cli, svc.serviceID, svc.initialReplicaCount)
					if err != nil {
						scaleUpErrors.append(err)
						return
					}

					scaledUpServices = append(scaledUpServices, svc)

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
				"Restarted %d out of %d stopped container(s).",
				len(restartedContainers),
				len(stoppedContainers),
			),
		)
		if s.isSwarm {
			s.logger.Info(
				fmt.Sprintf(
					"Scaled %d out of %d scaled down service(s) back up.",
					len(scaledUpServices),
					len(scaledDownServices),
				),
			)
		}

		return nil
	}, initialErr
}

func (s *script) determineContainersAndServicesToStop() error {
	allContainers, err := s.cli.ContainerList(context.Background(), client.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return errwrap.Wrap(err, "error querying for containers")
	}

	var containersToStop []handledContainer
	for _, c := range allContainers.Items {
		hasStopDuringBackupLabel, hasStopDuringBackupNoRestartLabel, err := checkStopLabels(c.Labels, s.c.BackupStopDuringBackupLabel, s.c.BackupStopDuringBackupNoRestartLabel)
		if err != nil {
			return errwrap.Wrap(err, "error querying for containers to stop")
		}

		if !hasStopDuringBackupLabel && !hasStopDuringBackupNoRestartLabel {
			continue
		}

		containersToStop = append(containersToStop, handledContainer{
			summary: c,
			restart: !hasStopDuringBackupNoRestartLabel,
		})
	}
	s.containersToStop = containersToStop

	var allServices []swarm.Service
	var servicesToScaleDown []handledSwarmService
	if s.isSwarm {
		result, err := s.cli.ServiceList(context.Background(), client.ServiceListOptions{Status: true})
		allServices = result.Items
		if err != nil {
			return errwrap.Wrap(err, "error querying for services")
		}

		for _, service := range allServices {
			hasStopDuringBackupLabel, hasStopDuringBackupNoRestartLabel, err := checkStopLabels(service.Spec.Labels, s.c.BackupStopDuringBackupLabel, s.c.BackupStopDuringBackupNoRestartLabel)
			if err != nil {
				return errwrap.Wrap(err, "error querying for services to scale down")
			}

			if !hasStopDuringBackupLabel && !hasStopDuringBackupNoRestartLabel {
				continue
			}

			if service.Spec.Mode.Replicated == nil {
				return errwrap.Wrap(
					nil,
					fmt.Sprintf("only replicated services can be restarted, but found a label on service %s", service.Spec.Name),
				)
			}

			servicesToScaleDown = append(servicesToScaleDown, handledSwarmService{
				serviceID:           service.ID,
				initialReplicaCount: *service.Spec.Mode.Replicated.Replicas,
				restart:             !hasStopDuringBackupNoRestartLabel,
			})
		}
	}
	s.servicesToScaleDown = servicesToScaleDown
	return nil
}
