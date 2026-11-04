// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/offen/envconfig"
)

func TestDeprecatedBackupStopContainerLabel(t *testing.T) {
	originalLookup := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = originalLookup })

	for _, test := range []struct {
		name              string
		values            map[string]string
		expectedLabel     string
		expectsDeprecated bool
		expectError       bool
	}{
		{
			name:              "deprecated key on its own",
			values:            map[string]string{"BACKUP_STOP_CONTAINER_LABEL": "database"},
			expectedLabel:     "database",
			expectsDeprecated: true,
		},
		{
			name: "deprecated key next to explicitly set successor",
			values: map[string]string{
				"BACKUP_STOP_CONTAINER_LABEL":     "database",
				"BACKUP_STOP_DURING_BACKUP_LABEL": "other",
			},
			expectError: true,
		},
		{
			name:          "successor key on its own",
			values:        map[string]string{"BACKUP_STOP_DURING_BACKUP_LABEL": "database"},
			expectedLabel: "database",
		},
		{
			name:          "defaults",
			expectedLabel: "true",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The conflict check looks at the environment, so a key that is meant to
			// be provided has to be present there as well.
			for key, value := range test.values {
				t.Setenv(key, value)
			}

			config, err := loadConfig(func(key string) (string, bool) {
				value, ok := test.values[key]
				return value, ok
			})
			if err != nil {
				t.Fatal(err)
			}

			unset, warnings, err := config.resolve()
			defer func() {
				if unsetErr := unset(); unsetErr != nil {
					t.Error(unsetErr)
				}
			}()

			if test.expectError {
				if err == nil {
					t.Fatal("Expected conflicting configuration to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.BackupStopDuringBackupLabel != test.expectedLabel {
				t.Errorf("Got label value %q, want %q", config.BackupStopDuringBackupLabel, test.expectedLabel)
			}

			hasDeprecationWarning := slices.ContainsFunc(warnings, func(warning string) bool {
				return strings.Contains(warning, "BACKUP_STOP_CONTAINER_LABEL has been deprecated")
			})
			if hasDeprecationWarning != test.expectsDeprecated {
				t.Errorf("Got deprecation warning = %v, want %v (warnings: %v)", hasDeprecationWarning, test.expectsDeprecated, warnings)
			}
		})
	}
}
