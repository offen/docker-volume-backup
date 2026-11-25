// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveMissingRootErrorText(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "missing")
	_, originalErr := os.Lstat(root)
	if originalErr == nil {
		t.Fatal("expected source root to be missing")
	}
	s := &script{
		c:      &Config{BackupSources: root},
		file:   filepath.Join(dir, "backup.tar"),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err := s.createArchive()
	want := fmt.Sprintf("backup.(*script).createArchive: error walking filesystem tree: %v", originalErr)
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
