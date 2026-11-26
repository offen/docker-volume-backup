// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/offen/envconfig"
)

func TestFailOnVolatileFilesystemConfiguration(t *testing.T) {
	previousLookup := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = previousLookup })

	configFile := filepath.Join(t.TempDir(), "option")
	if err := os.WriteFile(configFile, []byte("false"), 0600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		values  map[string]string
		want    bool
		wantErr bool
	}{
		{name: "unset remains strict", want: true},
		{name: "true remains strict", values: map[string]string{"BACKUP_FAIL_ON_VOLATILE_FILESYSTEM": "true"}, want: true},
		{name: "false permits missing entries", values: map[string]string{"BACKUP_FAIL_ON_VOLATILE_FILESYSTEM": "false"}},
		{name: "file provides value", values: map[string]string{"BACKUP_FAIL_ON_VOLATILE_FILESYSTEM_FILE": configFile}},
		{name: "conflicting values use existing provider behavior", values: map[string]string{"BACKUP_FAIL_ON_VOLATILE_FILESYSTEM": "false", "BACKUP_FAIL_ON_VOLATILE_FILESYSTEM_FILE": configFile}, want: true},
		{name: "missing file uses existing provider behavior", values: map[string]string{"BACKUP_FAIL_ON_VOLATILE_FILESYSTEM_FILE": filepath.Join(t.TempDir(), "missing")}, want: true},
		{name: "invalid value fails", values: map[string]string{"BACKUP_FAIL_ON_VOLATILE_FILESYSTEM": "invalid"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := loadConfig(func(key string) (string, bool) {
				if key == "BACKUP_SOURCES" {
					return "/existing-source", true
				}
				value, ok := tt.values[key]
				return value, ok
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected invalid configuration to fail")
				}
				return
			}
			if err != nil {
				t.Fatalf("load configuration: %v", err)
			}
			if config.BackupSources != "/existing-source" {
				t.Fatalf("existing source option changed: %q", config.BackupSources)
			}
			if got := config.BackupFailOnVolatileFilesystem; got != tt.want {
				t.Fatalf("fail on volatile filesystem = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCompressReturnsWarningsForMissingEntries(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	kept := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(kept, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, failOnMissing := range []bool{true, false} {
		name := "continue with warning"
		if failOnMissing {
			name = "fail by default"
		}
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "backup.tar")
			warnings, err := compress([]string{missing, kept}, archive, "none", 1, failOnMissing)
			if failOnMissing {
				if !errors.Is(err, os.ErrNotExist) || len(warnings) != 0 {
					t.Fatalf("strict mode: warnings=%v error=%v", warnings, err)
				}
				return
			}
			if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0].Error(), missing) {
				t.Fatalf("permissive mode: warnings=%v error=%v", warnings, err)
			}
			contents := readArchiveContents(t, archive)
			if len(contents) != 1 || contents["keep.txt"] != "kept" {
				t.Fatalf("unexpected archive contents: %v", contents)
			}
		})
	}
}
