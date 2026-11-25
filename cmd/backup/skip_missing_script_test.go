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
	script  *script
	root    string
	missing string
	volume  string
	logs    *bytes.Buffer
}

func newArchiveFixture(t *testing.T, skipMissing bool) archiveFixture {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "source")
	volume := filepath.Join(root, "volume")
	if err := os.MkdirAll(volume, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing.txt", "keep.txt", "volume/important.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = old })
	values := map[string]string{
		"BACKUP_SOURCES":     root,
		"BACKUP_COMPRESSION": "none",
	}
	if skipMissing {
		values["BACKUP_SKIP_MISSING_FILES"] = "true"
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
		script:  &script{c: config, file: filepath.Join(dir, "backup.tar"), logger: slog.New(slog.NewTextHandler(logs, nil))},
		root:    root,
		missing: filepath.Join(root, "missing.txt"),
		volume:  volume,
		logs:    logs,
	}
}

func readArchiveContents(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	reader := tar.NewReader(f)
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

func TestMissingSourceDirectoryIsNotSkipped(t *testing.T) {
	root := t.TempDir()
	volume := filepath.Join(root, "volume")
	if err := os.Mkdir(volume, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(volume)
	if err != nil {
		t.Fatal(err)
	}
	var logs, archive bytes.Buffer
	options := archiveOptions{
		skipMissingFiles:  true,
		sourceRoot:        root,
		sourceDirectories: map[string]os.FileInfo{volume: info},
		logger:            slog.New(slog.NewTextHandler(&logs, nil)),
	}
	operations := defaultArchiveFileOperations()
	operations.lstat = func(path string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
	}
	err = writeTarball(volume, tar.NewWriter(&archive), root, options, operations)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing directory: got %v, want os.ErrNotExist", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("directory must not be skipped: %q", logs.String())
	}
}

func TestScriptHandlesMissingFileAccordingToConfiguration(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newArchiveFixture(t, enabled)
			operations := defaultArchiveFileOperations()
			operations.lstat = func(path string) (os.FileInfo, error) {
				if path == fixture.missing {
					return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
				}
				return os.Lstat(path)
			}
			fixture.script.archiveLstat = operations.lstat
			err := fixture.script.createArchive()
			if _, statErr := os.Stat(fixture.missing); statErr != nil {
				t.Fatalf("source data changed: %v", statErr)
			}
			if !enabled {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("disabled option: got %v, want os.ErrNotExist", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("enabled option: %v", err)
			}
			if !strings.Contains(fixture.logs.String(), "WARN") || !strings.Contains(fixture.logs.String(), fixture.missing) {
				t.Fatalf("expected warning about missing file, got %q", fixture.logs.String())
			}
			contents := readArchiveContents(t, fixture.script.file)
			if _, present := contents["missing.txt"]; present {
				t.Fatal("missing file was included in archive")
			}
			if contents["keep.txt"] != "keep.txt" {
				t.Fatalf("kept file: contents=%q", contents["keep.txt"])
			}
		})
	}
}

func TestScriptRejectsMissingSourceDirectory(t *testing.T) {
	for _, stage := range []string{"before directory header", "after directory header", "replaced directory"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newArchiveFixture(t, true)
			operations := defaultArchiveFileOperations()
			volumeCalls := 0
			operations.lstat = func(path string) (os.FileInfo, error) {
				if path == fixture.volume {
					volumeCalls++
					if stage == "before directory header" || volumeCalls > 1 {
						if stage == "replaced directory" {
							return os.Lstat(fixture.root)
						}
						return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
					}
				}
				return os.Lstat(path)
			}
			fixture.script.archiveLstat = operations.lstat
			err := fixture.script.createArchive()
			if err == nil {
				t.Fatal("backup succeeded after a source directory disappeared or changed")
			}
			if stage != "replaced directory" && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing directory: got %v, want os.ErrNotExist", err)
			}
			if stage == "replaced directory" && !strings.Contains(err.Error(), "source directory changed") {
				t.Fatalf("unexpected replacement error: %v", err)
			}
			if strings.Contains(fixture.logs.String(), "Created backup of") {
				t.Fatal("backup was logged as successful")
			}
			if _, err := os.Stat(filepath.Join(fixture.volume, "important.txt")); err != nil {
				t.Fatalf("source data changed: %v", err)
			}
		})
	}
}

func TestScriptRejectsTransientDirectoryReplacement(t *testing.T) {
	fixture := newArchiveFixture(t, true)
	held := filepath.Join(filepath.Dir(fixture.root), "held")
	replacement := filepath.Join(filepath.Dir(fixture.root), "replacement")
	if err := os.Mkdir(replacement, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "important.txt"), []byte("replacement data"), 0600); err != nil {
		t.Fatal(err)
	}
	swapped, restored := false, false
	restore := func() {
		if !swapped || restored {
			return
		}
		if err := os.Rename(fixture.volume, replacement); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(held, fixture.volume); err != nil {
			t.Fatal(err)
		}
		restored = true
	}
	t.Cleanup(restore)
	operations := defaultArchiveFileOperations()
	operations.lstat = func(path string) (os.FileInfo, error) {
		if path == fixture.volume && !swapped {
			if err := os.Rename(fixture.volume, held); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, fixture.volume); err != nil {
				t.Fatal(err)
			}
			swapped = true
		}
		if path == fixture.root && swapped {
			restore()
		}
		return os.Lstat(path)
	}
	fixture.script.archiveLstat = operations.lstat
	err := fixture.script.createArchive()
	if !swapped {
		t.Fatal("replacement was not exercised")
	}
	restore()
	if err == nil || !strings.Contains(err.Error(), "source directory changed") {
		t.Fatalf("backup accepted replaced directory: %v", err)
	}
	if strings.Contains(fixture.logs.String(), "Created backup of") {
		t.Fatal("backup was logged as successful")
	}
	data, readErr := os.ReadFile(filepath.Join(fixture.volume, "important.txt"))
	if readErr != nil || string(data) != "volume/important.txt" {
		t.Fatalf("original source changed: %q, %v", data, readErr)
	}
}

func TestScriptRejectsSourceRootChangesAfterArchive(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		name := "missing"
		if replaced {
			name = "replaced"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newArchiveFixture(t, true)
			operations := defaultArchiveFileOperations()
			rootCalls := 0
			operations.lstat = func(path string) (os.FileInfo, error) {
				if path == fixture.root {
					rootCalls++
					if rootCalls > 2 {
						if replaced {
							return os.Lstat(fixture.volume)
						}
						return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
					}
				}
				return os.Lstat(path)
			}
			fixture.script.archiveLstat = operations.lstat
			err := fixture.script.createArchive()
			if !replaced && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing source root: got %v, want os.ErrNotExist", err)
			}
			if replaced && (err == nil || !strings.Contains(err.Error(), "source root changed")) {
				t.Fatalf("unexpected replacement error: %v", err)
			}
			if strings.Contains(fixture.logs.String(), "Created backup of") {
				t.Fatal("backup was logged as successful")
			}
		})
	}
}

func TestScriptExcludesDirectoryWhenSkippingEnabled(t *testing.T) {
	fixture := newArchiveFixture(t, true)
	fixture.script.c.BackupExcludeRegexp.Re = regexp.MustCompile("^" + regexp.QuoteMeta(fixture.volume))
	operations := defaultArchiveFileOperations()
	operations.lstat = func(path string) (os.FileInfo, error) {
		if path == fixture.volume {
			return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
		}
		return os.Lstat(path)
	}
	fixture.script.archiveLstat = operations.lstat
	if err := fixture.script.createArchive(); err != nil {
		t.Fatalf("excluded directory affected backup: %v", err)
	}
	contents := readArchiveContents(t, fixture.script.file)
	if _, present := contents["important.txt"]; present {
		t.Fatal("excluded file was included in archive")
	}
	if contents["keep.txt"] != "keep.txt" {
		t.Fatalf("kept file: contents=%q", contents["keep.txt"])
	}
}

func TestScriptKeepsIncludedFileWhenParentHeaderIsExcluded(t *testing.T) {
	fixture := newArchiveFixture(t, true)
	fixture.script.c.BackupExcludeRegexp.Re = regexp.MustCompile("^" + regexp.QuoteMeta(fixture.volume) + "$")
	if err := fixture.script.createArchive(); err != nil {
		t.Fatal(err)
	}
	contents := readArchiveContents(t, fixture.script.file)
	if _, present := contents["volume"]; present {
		t.Fatal("excluded directory header was included in archive")
	}
	if contents["important.txt"] != "volume/important.txt" {
		t.Fatalf("included file: contents=%q", contents["important.txt"])
	}
}

func TestScriptArchivesSingleSourceFileWhenSkippingEnabled(t *testing.T) {
	fixture := newArchiveFixture(t, true)
	fixture.script.c.BackupSources = fixture.missing
	if err := fixture.script.createArchive(); err != nil {
		t.Fatal(err)
	}
	contents := readArchiveContents(t, fixture.script.file)
	if len(contents) != 1 || contents["missing.txt"] != "missing.txt" {
		t.Fatalf("single source file: contents=%v", contents)
	}
}

func TestScriptRejectsMissingFileWithMissingParent(t *testing.T) {
	fixture := newArchiveFixture(t, true)
	operations := defaultArchiveFileOperations()
	important := filepath.Join(fixture.volume, "important.txt")
	volumeCalls := 0
	operations.lstat = func(path string) (os.FileInfo, error) {
		if path == important {
			return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
		}
		if path == fixture.volume {
			volumeCalls++
			if volumeCalls > 1 {
				return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
			}
		}
		return os.Lstat(path)
	}
	fixture.script.archiveLstat = operations.lstat
	err := fixture.script.createArchive()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing parent: got %v, want os.ErrNotExist", err)
	}
	if strings.Contains(fixture.logs.String(), "Skipping missing backup entry") {
		t.Fatal("file was skipped even though its parent disappeared")
	}
}

func TestScriptTracksExcludedParentOfIncludedFile(t *testing.T) {
	fixture := newArchiveFixture(t, true)
	fixture.script.c.BackupExcludeRegexp.Re = regexp.MustCompile("^" + regexp.QuoteMeta(fixture.volume) + "$")
	operations := defaultArchiveFileOperations()
	important := filepath.Join(fixture.volume, "important.txt")
	operations.lstat = func(path string) (os.FileInfo, error) {
		if path == important || path == fixture.volume {
			return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
		}
		return os.Lstat(path)
	}
	fixture.script.archiveLstat = operations.lstat
	err := fixture.script.createArchive()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing excluded parent of an included file: got %v, want os.ErrNotExist", err)
	}
	if strings.Contains(fixture.logs.String(), "Skipping missing backup entry") {
		t.Fatal("file was skipped even though its excluded parent disappeared")
	}
}

func TestScriptRejectsInaccessibleSourceDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("filesystem permissions cannot be tested as root or on Windows")
	}
	for _, skipMissing := range []bool{false, true} {
		name := "disabled"
		if skipMissing {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newArchiveFixture(t, skipMissing)
			if err := os.Chmod(fixture.volume, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(fixture.volume, 0700) })
			err := fixture.script.createArchive()
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("inaccessible directory: got %v, want os.ErrPermission", err)
			}
			var pathError *os.PathError
			if !errors.As(err, &pathError) || pathError.Op != "open" || pathError.Path != fixture.volume {
				t.Fatalf("permission error did not identify the inaccessible directory: %v", err)
			}
			if strings.Contains(fixture.logs.String(), "Created backup of") {
				t.Fatal("backup was logged as successful")
			}
			if err := os.Chmod(fixture.volume, 0700); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(fixture.volume, "important.txt"))
			if err != nil || string(data) != "volume/important.txt" {
				t.Fatalf("source data changed: %q, %v", data, err)
			}
		})
	}
}
