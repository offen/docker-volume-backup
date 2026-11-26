// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/offen/envconfig"
)

type archiveFixture struct {
	script *script
	root   string
	volume string
	logs   *bytes.Buffer
}

func newArchiveFixture(t *testing.T, failOnMissing bool) archiveFixture {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "source")
	volume := filepath.Join(root, "volume")
	if err := os.MkdirAll(volume, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"keep.txt", "volume/important.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	previousLookup := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = previousLookup })
	values := map[string]string{
		"BACKUP_SOURCES":     root,
		"BACKUP_COMPRESSION": "none",
	}
	if !failOnMissing {
		values["BACKUP_FAIL_ON_VOLATILE_FILESYSTEM"] = "false"
	}
	config, err := loadConfig(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	return archiveFixture{
		script: &script{c: config, file: filepath.Join(dir, "backup.tar"), logger: slog.New(slog.NewTextHandler(logs, nil))},
		root:   root, volume: volume, logs: logs,
	}
}

func readArchiveContents(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	reader := tar.NewReader(file)
	contents := make(map[string]string)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return contents
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		contents[filepath.Base(header.Name)] = string(data)
	}
}

func TestScriptExcludesDirectory(t *testing.T) {
	for _, failOnMissing := range []bool{true, false} {
		fixture := newArchiveFixture(t, failOnMissing)
		fixture.script.c.BackupExcludeRegexp.Re = regexp.MustCompile("^" + regexp.QuoteMeta(fixture.volume))
		if err := fixture.script.createArchive(); err != nil {
			t.Fatal(err)
		}
		contents := readArchiveContents(t, fixture.script.file)
		if _, present := contents["important.txt"]; present || contents["keep.txt"] != "keep.txt" {
			t.Fatalf("unexpected archive contents: %v", contents)
		}
	}
}

func TestScriptKeepsIncludedFileWhenParentHeaderIsExcluded(t *testing.T) {
	fixture := newArchiveFixture(t, false)
	fixture.script.c.BackupExcludeRegexp.Re = regexp.MustCompile("^" + regexp.QuoteMeta(fixture.volume) + "$")
	if err := fixture.script.createArchive(); err != nil {
		t.Fatal(err)
	}
	contents := readArchiveContents(t, fixture.script.file)
	if _, present := contents["volume"]; present || contents["important.txt"] != "volume/important.txt" {
		t.Fatalf("unexpected archive contents: %v", contents)
	}
}

func TestScriptArchivesSingleSourceFile(t *testing.T) {
	fixture := newArchiveFixture(t, false)
	fixture.script.c.BackupSources = filepath.Join(fixture.root, "keep.txt")
	if err := fixture.script.createArchive(); err != nil {
		t.Fatal(err)
	}
	contents := readArchiveContents(t, fixture.script.file)
	if len(contents) != 1 || contents["keep.txt"] != "keep.txt" {
		t.Fatalf("unexpected archive contents: %v", contents)
	}
}

func TestScriptRejectsMissingSourceRoot(t *testing.T) {
	for _, failOnMissing := range []bool{true, false} {
		fixture := newArchiveFixture(t, failOnMissing)
		fixture.script.c.BackupSources = filepath.Join(fixture.root, "missing")
		if err := fixture.script.createArchive(); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing source root: %v", err)
		}
		if _, err := os.Stat(fixture.script.file); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected archive for missing root: %v", err)
		}
	}
}

func TestScriptRejectsInaccessibleSourceDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("filesystem permissions cannot be tested as root or on Windows")
	}
	for _, failOnMissing := range []bool{true, false} {
		fixture := newArchiveFixture(t, failOnMissing)
		if err := os.Chmod(fixture.volume, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(fixture.volume, 0700) })
		err := fixture.script.createArchive()
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("inaccessible directory: %v", err)
		}
		var pathError *os.PathError
		if !errors.As(err, &pathError) || pathError.Op != "open" || pathError.Path != fixture.volume {
			t.Fatalf("permission error did not identify directory: %v", err)
		}
		if strings.Contains(fixture.logs.String(), "Created backup of") {
			t.Fatal("backup was logged as successful")
		}
	}
}
