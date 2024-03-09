// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"

	"github.com/offen/docker-volume-backup/internal/errwrap"
)

// runScript instantiates a new script object and orchestrates a backup run.
// To ensure it runs mutually exclusive a global file lock is acquired before
// it starts running. Any panic within the script will be recovered and returned
// as an error.
func runScript(c *Config) (err error) {
	defer func() {
		if derr := recover(); derr != nil {
			asErr, ok := derr.(error)
			if ok {
				err = errwrap.Wrap(asErr, "unexpected panic running script")
			} else {
				err = errwrap.Wrap(nil, fmt.Sprintf("%v", derr))
			}
		}
	}()

	s := newScript(c)

	return func() (err error) {
		scriptErr := func() (err error) {
			unset, err := s.c.applyEnv()
			if err != nil {
				err = errwrap.Wrap(err, "error applying env")
				return
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

			err = s.withLabeledCommands(lifecyclePhaseProcess, s.encryptArchive)()
			if err != nil {
				err = errwrap.Wrap(err, "error encrypting archive")
				return
			}
			err = s.withLabeledCommands(lifecyclePhaseCopy, s.copyArchive)()
			if err != nil {
				err = errwrap.Wrap(err, "error copying archive")
				return
			}
			err = s.withLabeledCommands(lifecyclePhasePrune, s.pruneBackups)()
			if err != nil {
				err = errwrap.Wrap(err, "error pruning backups")
				return
			}

			return
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
