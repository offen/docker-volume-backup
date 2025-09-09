// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

// Portions of this file are taken and adapted from `moby`, Copyright 2012-2017 Docker, Inc.
// Licensed under the Apache 2.0 License: https://github.com/moby/moby/blob/8e610b2b55bfd1bfa9436ab110d311f5e8a74dcb/LICENSE

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cosiner/argv"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/offen/docker-volume-backup/internal/errwrap"
	"golang.org/x/sync/errgroup"
)

func (s *script) exec(containerRef string, command string, user string) ([]byte, []byte, error) {
	args, err := argv.Argv(command, nil, nil)
	if err != nil {
		return nil, nil, errwrap.Wrap(err, fmt.Sprintf("error parsing argv from '%s'", command))
	}
	if len(args) == 0 {
		return nil, nil, errwrap.Wrap(nil, "received unexpected empty command")
	}

	commandEnv := []string{
		fmt.Sprintf("COMMAND_RUNTIME_ARCHIVE_FILEPATH=%s", s.file),
	}

	execID, err := s.cli.ContainerExecCreate(context.Background(), containerRef, container.ExecOptions{
		Cmd:          args[0],
		AttachStdin:  true,
		AttachStderr: true,
		Env:          commandEnv,
		User:         user,
	})
	if err != nil {
		return nil, nil, errwrap.Wrap(err, "error creating container exec")
	}

	resp, err := s.cli.ContainerExecAttach(context.Background(), execID.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, nil, errwrap.Wrap(err, "error attaching container exec")
	}
	defer resp.Close()

	var outBuf, errBuf, fullRespBuf bytes.Buffer
	outputDone := make(chan error)

	tee := io.TeeReader(resp.Reader, &fullRespBuf)

	go func() {
		_, err := stdcopy.StdCopy(&outBuf, &errBuf, tee)
		outputDone <- err
	}()

	if err := <-outputDone; err != nil {
		if body, bErr := io.ReadAll(&fullRespBuf); bErr == nil {
			// if possible, try to append the exec output to the error
			// as it's likely to be more relevant for users than the error from
			// calling stdcopy.Copy
			err = errwrap.Wrap(errors.New(string(body)), err.Error())
		}
		return nil, nil, errwrap.Wrap(err, "error demultiplexing output")
	}

	stdout, err := io.ReadAll(&outBuf)
	if err != nil {
		return nil, nil, errwrap.Wrap(err, "error reading stdout")
	}
	stderr, err := io.ReadAll(&errBuf)
	if err != nil {
		return nil, nil, errwrap.Wrap(err, "error reading stderr")
	}

	res, err := s.cli.ContainerExecInspect(context.Background(), execID.ID)
	if err != nil {
		return nil, nil, errwrap.Wrap(err, "error inspecting container exec")
	}

	if res.ExitCode > 0 {
		return stdout, stderr, errwrap.Wrap(nil, fmt.Sprintf("running command exited %d", res.ExitCode))
	}

	return stdout, stderr, nil
}

func (s *script) runLabeledCommands(label string) error {
	f := []filters.KeyValuePair{
		{Key: "label", Value: label},
	}
	if s.c.ExecLabel != "" {
		f = append(f, filters.KeyValuePair{
			Key:   "label",
			Value: fmt.Sprintf("docker-volume-backup.exec-label=%s", s.c.ExecLabel),
		})
	}
	containersWithCommand, err := s.cli.ContainerList(context.Background(), container.ListOptions{
		Filters: filters.NewArgs(f...),
	})
	if err != nil {
		return errwrap.Wrap(err, "error querying for containers")
	}

	var hasDeprecatedContainers bool
	if label == "docker-volume-backup.archive-pre" {
		f[0] = filters.KeyValuePair{
			Key:   "label",
			Value: "docker-volume-backup.exec-pre",
		}
		deprecatedContainers, err := s.cli.ContainerList(context.Background(), container.ListOptions{
			Filters: filters.NewArgs(f...),
		})
		if err != nil {
			return errwrap.Wrap(err, "error querying for containers")
		}
		if len(deprecatedContainers) != 0 {
			hasDeprecatedContainers = true
			containersWithCommand = append(containersWithCommand, deprecatedContainers...)
		}
	}

	if label == "docker-volume-backup.archive-post" {
		f[0] = filters.KeyValuePair{
			Key:   "label",
			Value: "docker-volume-backup.exec-post",
		}
		deprecatedContainers, err := s.cli.ContainerList(context.Background(), container.ListOptions{
			Filters: filters.NewArgs(f...),
		})
		if err != nil {
			return errwrap.Wrap(err, "error querying for containers")
		}
		if len(deprecatedContainers) != 0 {
			hasDeprecatedContainers = true
			containersWithCommand = append(containersWithCommand, deprecatedContainers...)
		}
	}

	if len(containersWithCommand) == 0 {
		return nil
	}

	if hasDeprecatedContainers {
		s.logger.Warn(
			"Using `docker-volume-backup.exec-pre` and `docker-volume-backup.exec-post` labels has been deprecated and will be removed in the next major version.",
		)
		s.logger.Warn(
			"Please use other `-pre` and `-post` labels instead. Refer to the README for an upgrade guide.",
		)
	}

	g := new(errgroup.Group)

	for _, container := range containersWithCommand {
		c := container
		g.Go(func() error {
			cmd, ok := c.Labels[label]
			if !ok && label == "docker-volume-backup.archive-pre" {
				cmd = c.Labels["docker-volume-backup.exec-pre"]
			} else if !ok && label == "docker-volume-backup.archive-post" {
				cmd = c.Labels["docker-volume-backup.exec-post"]
			}

			userLabelName := fmt.Sprintf("%s.user", label)
			user := c.Labels[userLabelName]

			s.logger.Info(fmt.Sprintf("Running %s command %s for container %s", label, cmd, strings.TrimPrefix(c.Names[0], "/")))
			stdout, stderr, err := s.exec(c.ID, cmd, user)
			if s.c.ExecForwardOutput {
				if _, err := os.Stderr.Write(stderr); err != nil {
					return errwrap.Wrap(err, "error writing to stderr")
				}
				if _, err := os.Stdout.Write(stdout); err != nil {
					return errwrap.Wrap(err, "error writing to stdout")
				}
			}
			if err != nil {
				return errwrap.Wrap(err, "error executing command")
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return errwrap.Wrap(err, "error from errgroup")
	}
	return nil
}

type lifecyclePhase string

const (
	lifecyclePhaseArchive lifecyclePhase = "archive"
	lifecyclePhaseProcess lifecyclePhase = "process"
	lifecyclePhaseCopy    lifecyclePhase = "copy"
	lifecyclePhasePrune   lifecyclePhase = "prune"
)

func (s *script) withLabeledCommands(step lifecyclePhase, cb func() error) func() error {
	if s.cli == nil {
		return cb
	}
	return func() (err error) {
		if err = s.runLabeledCommands(fmt.Sprintf("docker-volume-backup.%s-pre", step)); err != nil {
			err = errwrap.Wrap(err, fmt.Sprintf("error running %s-pre commands", step))
			return
		}
		defer func() {
			if derr := s.runLabeledCommands(fmt.Sprintf("docker-volume-backup.%s-post", step)); derr != nil {
				err = errors.Join(err, errwrap.Wrap(derr, fmt.Sprintf("error running %s-post commands", step)))
			}
		}()
		err = cb()
		return
	}
}
