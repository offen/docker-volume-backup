// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
)

func main() {
	s, err := newScript()
	if err != nil {
		panic(err)
	}

	unlock, err := s.lock("/var/lock/dockervolumebackup.lock")
	defer s.must(unlock())
	s.must(err)

	defer func() {
		if pArg := recover(); pArg != nil {
			if err, ok := pArg.(error); ok {
				s.logger.Error(
					fmt.Sprintf("Executing the script encountered a panic: %v", err),
				)
				if hookErr := s.runHooks(err); hookErr != nil {
					s.logger.Error(
						fmt.Sprintf("An error occurred calling the registered hooks: %s", hookErr),
					)
				}
				os.Exit(1)
			}
			panic(pArg)
		}

		if err := s.runHooks(nil); err != nil {
			s.logger.Error(
				fmt.Sprintf(
					"Backup procedure ran successfully, but an error ocurred calling the registered hooks: %v",
					err,
				),
			)
			os.Exit(1)
		}
		s.logger.Info("Finished running backup tasks.")
	}()

	s.must(s.withLabeledCommands(lifecyclePhaseArchive, func() error {
		restartContainers, err := s.stopContainers()
		// The mechanism for restarting containers is not using hooks as it
		// should happen as soon as possible (i.e. before uploading backups or
		// similar).
		defer func() {
			s.must(restartContainers())
		}()
		if err != nil {
			return err
		}
		return s.createArchive()
	})())

	s.must(s.withLabeledCommands(lifecyclePhaseProcess, s.encryptArchive)())
	s.must(s.withLabeledCommands(lifecyclePhaseCopy, s.copyArchive)())
	s.must(s.withLabeledCommands(lifecyclePhasePrune, s.pruneBackups)())
}
