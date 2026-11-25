// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

//go:build linux

package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/offen/envconfig"
	"golang.org/x/sys/unix"
)

func TestSkipMissingSourceRootRemovedAfterListing(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "removed"
		if replace {
			name = "replaced"
		}
		t.Run(name, func(t *testing.T) {
			testSkipMissingSourceRootChanged(t, replace)
		})
	}
}

func testSkipMissingSourceRootChanged(t *testing.T, replace bool) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "source")
	displaced := filepath.Join(dir, "displaced")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "a.dat")
	f, err := os.Create(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	contents := []byte("essential data")
	if err := os.WriteFile(filepath.Join(root, "z.txt"), contents, 0600); err != nil {
		t.Fatal(err)
	}

	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, first, unix.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		_, err := unix.Read(fd, buf)
		if err == nil {
			err = os.Rename(root, displaced)
		}
		if err == nil && replace {
			err = os.Mkdir(root, 0700)
		}
		removed <- err
	}()

	old := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = old })
	config, err := loadConfig(func(key string) (string, bool) {
		values := map[string]string{
			"BACKUP_SKIP_MISSING_FILES": "true",
			"BACKUP_SOURCES":            root,
			"BACKUP_COMPRESSION":        "gz",
		}
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	archive := filepath.Join(dir, "out.tar.gz")
	s := &script{c: config, file: archive, logger: slog.New(slog.NewTextHandler(&logs, nil))}
	archiveErr := s.createArchive()
	select {
	case err := <-removed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("source file was never opened")
	}
	remaining, err := os.ReadFile(filepath.Join(displaced, "z.txt"))
	if err != nil || !bytes.Equal(remaining, contents) {
		t.Fatalf("displaced source changed: contents=%q, error=%v", remaining, err)
	}
	if archiveErr == nil {
		if strings.Contains(logs.String(), "Skipping missing backup entry") {
			t.Fatalf("backup succeeded after the source root changed; logs: %s", logs.String())
		}
		t.Skip("source changed after the archive was complete")
	}
	if strings.Contains(logs.String(), "Created backup of") {
		t.Fatalf("backup logged success after the source root changed: %s", logs.String())
	}
}
