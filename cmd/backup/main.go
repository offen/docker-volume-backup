// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
)

func main() {
	unlock := lock("/var/lock/dockervolumebackup.lock")
	defer unlock()

	s, err := newScript()
	if err != nil {
		panic(err)
	}

	defer func() {
		if pArg := recover(); pArg != nil {
			if err, ok := pArg.(error); ok {
				if hookErr := s.runHooks(err); hookErr != nil {
					s.logger.Errorf("An error occurred calling the registered hooks: %s", hookErr)
				}
				os.Exit(1)
			}
			panic(pArg)
		}

		if err := s.runHooks(nil); err != nil {
			s.logger.Errorf(
				"Backup procedure ran successfully, but an error ocurred calling the registered hooks: %v",
				err,
			)
			os.Exit(1)
		}
		s.logger.Info("Finished running backup tasks.")
	}()

	s.must(func() error {
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
		return s.takeBackup()
	}())

	s.must(s.encryptBackup())
	s.must(s.copyBackup())
	s.must(s.pruneOldBackups())
}
