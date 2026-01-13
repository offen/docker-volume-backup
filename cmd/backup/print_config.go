// Copyright 2025 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
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
		if err := func() (err error) {
			unset, warnings, err := config.resolve()
			if err != nil {
				return errwrap.Wrap(err, "error resolving configuration")
			}
			defer func() {
				if derr := unset(); derr != nil {
					err = errors.Join(err, errwrap.Wrap(derr, "error unsetting environment variables"))
				}
			}()

			fmt.Printf("source=%s\n", config.source)
			for _, warning := range warnings {
				fmt.Printf("warning:%s\n", warning)
			}
			// insert line breaks before each field name, assuming field names start with uppercase letters
			formatted := formatter.ReplaceAllString(fmt.Sprintf("%+v", *config), "\n$1")
			fmt.Printf("%s\n", formatted)
			return nil
		}(); err != nil {
			return err
		}
	}

	return nil
}
