// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
