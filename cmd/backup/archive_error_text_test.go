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

func TestArchiveOutputErrorKeepsExistingContext(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	output := filepath.Join(dir, "output-directory")
	for _, path := range []string{source, output} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	_, openErr := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if openErr == nil {
		t.Fatal("expected opening the output directory to fail")
	}
	s := &script{
		c:      &Config{BackupSources: source, BackupCompression: "none"},
		file:   output,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err := s.createArchive()
	want := fmt.Sprintf("backup.(*script).createArchive: error compressing backup folder: backup.createArchive: error compressing archive: backup.compress: error creating out file: %v", openErr)
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
