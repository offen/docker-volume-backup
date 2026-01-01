// Copyright 2025 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"regexp"

	"github.com/offen/docker-volume-backup/internal/errwrap"
)

func runPrintConfig() error {
	configurations, err := sourceConfiguration(configStrategyConfd)
	if err != nil {
		fmt.Printf("error sourcing configuration: %v\n", err) // print error to stdout for debugging
		return errwrap.Wrap(err, "error sourcing configuration")
	}

	for _, config := range configurations {
		if config.BackupFilenameExpand {
			unset, err := config.applyEnv()
			if err != nil {
				fmt.Printf("error applying env: %v\n", err) // print error to stdout for debugging
				return errwrap.Wrap(err, "error applying env")
			}
			config.BackupFilename = os.ExpandEnv(config.BackupFilename)
			config.BackupLatestSymlink = os.ExpandEnv(config.BackupLatestSymlink)
			config.BackupPruningPrefix = os.ExpandEnv(config.BackupPruningPrefix)
			if err := unset(); err != nil {
				fmt.Printf("error unsetting env: %v\n", err) // print error to stdout for debugging
				return errwrap.Wrap(err, "error unsetting environment variables")
			}
		}
		// insert line breaks before each field name, assuming field names start with uppercase letters
		formatted := regexp.MustCompile(`\s([A-Z])`).ReplaceAllString(fmt.Sprintf("%+v", *config), "\n$1")
		fmt.Printf("source=%s\n%s\n", config.source, formatted)
	}

	return nil
}
