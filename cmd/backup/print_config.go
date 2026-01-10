// Copyright 2025 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"regexp"

	"github.com/offen/docker-volume-backup/internal/errwrap"
)

func runPrintConfig() error {
	configurations, err := sourceConfiguration(configStrategyConfd)
	if err != nil {
		return errwrap.Wrap(err, "error sourcing configuration")
	}

	formatter := regexp.MustCompile(`\s([A-Z])`)
	for _, config := range configurations {
		if err := func() error {
			unset, warnings, err := config.resolve()
			if err != nil {
				if unset != nil {
					_ = unset()
				}
				return errwrap.Wrap(err, "error resolving configuration")
			}
			fmt.Printf("source=%s\n", config.source)
			for _, warning := range warnings {
				fmt.Printf("warning:%s\n", warning)
			}
			// insert line breaks before each field name, assuming field names start with uppercase letters
			formatted := formatter.ReplaceAllString(fmt.Sprintf("%+v", *config), "\n$1")
			fmt.Printf("%s\n", formatted)
			if err := unset(); err != nil {
				return errwrap.Wrap(err, "error unsetting environment variables")
			}
			return nil
		}(); err != nil {
			return err
		}
	}

	return nil
}
