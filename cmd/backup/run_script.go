// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"
)

// runScript instantiates a new script object and orchestrates a backup run.
// To ensure it runs mutually exclusive a global file lock is acquired before
// it starts running. Any panic within the script will be recovered and returned
// as an error.
func runScript(c *Config) (err error) {
	defer func() {
		if derr := recover(); derr != nil {
			err = fmt.Errorf("runScript: unexpected panic running script: %v", derr)
		}
	}()

	s := newScript(c)

	unlock, lockErr := s.lock("/var/lock/dockervolumebackup.lock")
	if lockErr != nil {
		err = fmt.Errorf("runScript: error acquiring file lock: %w", lockErr)
		return
	}
	defer func() {
		if derr := unlock(); derr != nil {
			err = errors.Join(err, fmt.Errorf("runScript: error releasing file lock: %w", derr))
		}
	}()

	unset, err := s.c.applyEnv()
	if err != nil {
		return fmt.Errorf("runScript: error applying env: %w", err)
	}
	defer func() {
		if derr := unset(); derr != nil {
			err = errors.Join(err, fmt.Errorf("runScript: error unsetting environment variables: %w", derr))
		}
	}()

	if initErr := s.init(); initErr != nil {
		err = fmt.Errorf("runScript: error instantiating script: %w", initErr)
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
						err = errors.Join(err, fmt.Errorf("runScript: error restarting containers and services: %w", derr))
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
				return fmt.Errorf(
					"runScript: error %w executing the script followed by %w calling the registered hooks",
					scriptErr,
					hookErr,
				)
			}
			return fmt.Errorf(
				"runScript: the script ran successfully, but an error occurred calling the registered hooks: %w",
				hookErr,
			)
		}
		if scriptErr != nil {
			return fmt.Errorf("runScript: error running script: %w", scriptErr)
		}
		return nil
	}()
}
