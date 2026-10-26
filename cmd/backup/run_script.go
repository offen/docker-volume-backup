// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"
	"math/rand"
	"runtime/debug"
	"time"

	"github.com/offen/docker-volume-backup/internal/errwrap"
)

type runScriptOpts struct {
	// scheduled is set when the run has been triggered by a cron schedule
	// instead of being invoked manually. Only scheduled runs honor the
	// configured jitter.
	scheduled bool
}

// jitterDelay returns a random delay between 0 and the given maximum.
// Runs that were not triggered by a schedule are never delayed.
func jitterDelay(max time.Duration, scheduled bool) time.Duration {
	if !scheduled || max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max) + 1))
}

// runScript instantiates a new script object and orchestrates a backup run.
// To ensure it runs mutually exclusive a global file lock is acquired before
// it starts running. Any panic within the script will be recovered and returned
// as an error.
func runScript(c *Config, opts runScriptOpts) (err error) {
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

	unset, warnings, err := s.c.resolve()
	if err != nil {
		return errwrap.Wrap(err, "error applying env")
	}
	defer func() {
		if derr := unset(); derr != nil {
			err = errors.Join(err, errwrap.Wrap(derr, "error unsetting environment variables"))
		}
	}()
	for _, w := range warnings {
		s.logger.Warn(w)
	}

	if s.c != nil {
		if delay := jitterDelay(s.c.BackupJitter, opts.scheduled); delay > 0 {
			s.logger.Info(fmt.Sprintf("Applying startup jitter of %v", delay))
			time.Sleep(delay)
		}
	}

	if initErr := s.init(); initErr != nil {
		if hookErr := s.runHooks(initErr); hookErr != nil {
			err = errwrap.Wrap(
				nil,
				fmt.Sprintf(
					"error %v instantiating script followed by %v calling the registered hooks",
					initErr,
					hookErr,
				),
			)
			return
		}
		err = errwrap.Wrap(initErr, "error instantiating script")
		return
	}

	err = func() (err error) {
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

	return
}
