// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
)

// runScript instantiates a new script object and orchestrates a backup run.
// To ensure it runs mutually exclusive a global file lock is acquired before
// it starts running. Any panic within the script will be recovered and returned
// as an error.
func runScript(c *Config) (err error) {
	defer func() {
		if derr := recover(); derr != nil {
			fmt.Printf("%s: %s\n", derr, debug.Stack())
			asErr, ok := derr.(error)
			if ok {
				err = errwrap.Wrap(asErr, "unexpected panic running script")
			} else {
				err = errwrap.Wrap(nil, fmt.Sprintf("%v", derr))
			}
		}
	}()

	s := newScript(c)

	unlock, lockErr := s.lock("/var/lock/dockervolumebackup.lock")
	if lockErr != nil {
		err = errwrap.Wrap(lockErr, "error acquiring file lock")
		return
	}
	defer func() {
		if derr := unlock(); derr != nil {
			err = errors.Join(err, errwrap.Wrap(derr, "error releasing file lock"))
		}
	}()

	unset, err := s.c.applyEnv()
	if err != nil {
		return errwrap.Wrap(err, "error applying env")
	}
	defer func() {
		if derr := unset(); derr != nil {
			err = errors.Join(err, errwrap.Wrap(derr, "error unsetting environment variables"))
		}
	}()

	if initErr := s.init(); initErr != nil {
		err = errwrap.Wrap(initErr, "error instantiating script")
		return
	}

	return func() (err error) {
		scriptErr := func() error {
			if err := s.withLabeledCommands(lifecyclePhaseArchive, func() (err error) {
				restartContainersAndServices, err := s.stopContainersAndServices()
				// The mechanism for restarting containers is not using hooks as it
				// should happen as soon as possible (i.e. before uploading backups or
				// similar).
				defer func() {
					if derr := restartContainersAndServices(); derr != nil {
						err = errors.Join(err, errwrap.Wrap(derr, "error restarting containers and services"))
					}
				}()
				if err != nil {
					return
				}
				err = s.createArchive()
				return
			})(); err != nil {
				return err
			}

			if err := s.withLabeledCommands(lifecyclePhaseProcess, s.encryptArchive)(); err != nil {
				return err
			}
			if err := s.withLabeledCommands(lifecyclePhaseCopy, s.copyArchive)(); err != nil {
				return err
			}
			if err := s.withLabeledCommands(lifecyclePhasePrune, s.pruneBackups)(); err != nil {
				return err
			}
			return nil
		}()

		if hookErr := s.runHooks(scriptErr); hookErr != nil {
			if scriptErr != nil {
				return errwrap.Wrap(
					nil,
					fmt.Sprintf(
						"error %v executing the script followed by %v calling the registered hooks",
						scriptErr,
						hookErr,
					),
				)
			}
			return errwrap.Wrap(
				hookErr,
				"the script ran successfully, but an error occurred calling the registered hooks",
			)
		}
		if scriptErr != nil {
			return errwrap.Wrap(scriptErr, "error running script")
		}
		return nil
	}()
}
