package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/offen/docker-volume-backup/internal/errwrap"
)

type StopInfo struct {
	ContainerIDs    []string `json:"containerIds"`
	SwarmServiceIDs []string `json:"swarmServiceIds"`
}

// writeWaitingFile creates a file with the name of the script's id and the suffix .waiting in the same directory as the lock file.
// The file contains a JSON array with the IDs of the containers that will be stopped and services that will be scaled down for this backup run.
// This file can be used by external processes to determine which containers or services are being stopped in currently waiting backup runs.
func (s *script) writeWaitingFile() error {
	lockDir := filepath.Dir(LOCK_FILE)
	waitingFilePath := filepath.Join(lockDir, s.id+".waiting")

	_, err := os.Stat(waitingFilePath)
	if err != nil && !os.IsNotExist(err) {
		return errwrap.Wrap(err, "error checking for existence of waiting file")
	}

	if os.IsNotExist(err) {

		stopInfo := StopInfo{
			ContainerIDs:    []string{},
			SwarmServiceIDs: []string{},
		}
		for _, container := range s.containersToStop {
			stopInfo.ContainerIDs = append(stopInfo.ContainerIDs, container.summary.ID)
		}
		for _, service := range s.servicesToScaleDown {
			stopInfo.SwarmServiceIDs = append(stopInfo.SwarmServiceIDs, service.serviceID)
		}

		data, err := json.Marshal(stopInfo)
		if err != nil {
			return errwrap.Wrap(err, "error marshalling stop info")
		}
		if err := os.WriteFile(waitingFilePath, data, 0644); err != nil {
			return errwrap.Wrap(err, "error writing waiting file")
		}
	}
	return nil
}

// removeWaitingFile removes the waiting file that was created for this backup run.
func (s *script) removeWaitingFile() error {
	lockDir := filepath.Dir(LOCK_FILE)
	waitingFilePath := filepath.Join(lockDir, s.id+".waiting")

	if err := remove(waitingFilePath); err != nil {
		return errwrap.Wrap(err, "error removing waiting file")
	}
	return nil
}

// loadContainerIdSAndServiceIDsToStop looks for all waiting files in the lock file directory, loads the IDs of containers and services to stop from these files and returns them as slices of strings.
func (s *script) loadContainerIdSAndServiceIDsToStop() (map[string]bool, map[string]bool, error) {
	containerIDsToStop := make(map[string]bool)
	serviceIDsToScaleDown := make(map[string]bool)

	lockDir := filepath.Dir(LOCK_FILE)
	files, err := filepath.Glob(filepath.Join(lockDir, "*.waiting"))
	if err != nil {
		return nil, nil, errwrap.Wrap(err, "error listing waiting files")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, errwrap.Wrap(err, "error reading waiting file")
		}
		var stopInfo StopInfo
		if err := json.Unmarshal(data, &stopInfo); err != nil {
			return nil, nil, errwrap.Wrap(err, "error unmarshalling waiting file")
		}
		for _, containerID := range stopInfo.ContainerIDs {
			containerIDsToStop[containerID] = true
		}
		for _, serviceID := range stopInfo.SwarmServiceIDs {
			serviceIDsToScaleDown[serviceID] = true
		}
	}
	return containerIDsToStop, serviceIDsToScaleDown, nil
}
