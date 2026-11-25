// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/offen/envconfig"
)

func TestSkipMissingExplicitConfigurationIsNotIgnored(t *testing.T) {
	old := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = old })
	dir := t.TempDir()
	file := filepath.Join(dir, "flag")
	if err := os.WriteFile(file, []byte("false"), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		values map[string]string
	}{
		{"conflicting values", map[string]string{"BACKUP_SKIP_MISSING_FILES": "true", "BACKUP_SKIP_MISSING_FILES_FILE": file}},
		{"invalid explicit value", map[string]string{"BACKUP_SKIP_MISSING_FILES": "bad", "BACKUP_SKIP_MISSING_FILES_FILE": file}},
		{"missing value file", map[string]string{"BACKUP_SKIP_MISSING_FILES_FILE": filepath.Join(dir, "absent")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := loadConfig(func(key string) (string, bool) {
				value, ok := tt.values[key]
				return value, ok
			})
			if err == nil {
				t.Errorf("explicit invalid/conflicting configuration ignored: skip=%t", config.BackupSkipMissingFiles)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "false" {
				t.Fatalf("value file changed: %q, %v", data, err)
			}
		})
	}
}

func TestSkipMissingConfigurationFromFile(t *testing.T) {
	old := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = old })
	file := filepath.Join(t.TempDir(), "flag")
	if err := os.WriteFile(file, []byte("true"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(func(key string) (string, bool) {
		if key == "BACKUP_SKIP_MISSING_FILES_FILE" {
			return file, true
		}
		return "", false
	})
	if err != nil || !config.BackupSkipMissingFiles {
		t.Fatalf("expected enabled configuration from file: config=%v, error=%v", config, err)
	}
}
