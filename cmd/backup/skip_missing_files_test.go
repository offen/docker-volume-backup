// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/offen/envconfig"
)

func TestBackupSkipMissingFilesConfiguration(t *testing.T) {
	previousLookup := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = previousLookup })

	tests := []struct {
		name    string
		value   string
		present bool
		want    bool
		wantErr bool
	}{
		{name: "unset defaults to fail-fast"},
		{name: "false preserves fail-fast", value: "false", present: true},
		{name: "true enables skipping", value: "true", present: true, want: true},
		{name: "invalid value fails", value: "invalid", present: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := loadConfig(func(key string) (string, bool) {
				if key == "BACKUP_SKIP_MISSING_FILES" {
					return tt.value, tt.present
				}
				return "", false
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

			if got := config.BackupSkipMissingFiles; got != tt.want {
				t.Fatalf("skip missing files = %t, want %t", got, tt.want)
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

func TestSkipMissingInvalidFileConfiguration(t *testing.T) {
	old := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = old })
	dir := t.TempDir()
	file := filepath.Join(dir, "flag")
	if err := os.WriteFile(file, []byte("false"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		values map[string]string
	}{
		{"conflicting values", map[string]string{"BACKUP_SKIP_MISSING_FILES": "true", "BACKUP_SKIP_MISSING_FILES_FILE": file}},
		{"invalid explicit value", map[string]string{"BACKUP_SKIP_MISSING_FILES": "bad", "BACKUP_SKIP_MISSING_FILES_FILE": file}},
		{"missing value file", map[string]string{"BACKUP_SKIP_MISSING_FILES_FILE": filepath.Join(dir, "absent")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(func(key string) (string, bool) {
				value, ok := tt.values[key]
				return value, ok
			})
			if err == nil {
				t.Fatal("explicit invalid configuration was ignored")
			}
		})
	}
}

func TestMissingEntryBeforeHeaderLeavesValidArchive(t *testing.T) {
	for _, kind := range []string{"file", "empty file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			vanished := filepath.Join(dir, "vanished")
			keep := filepath.Join(dir, "keep.txt")
			if kind != "symlink" {
				contents := []byte("lost")
				if kind == "empty file" {
					contents = nil
				}
				if err := os.WriteFile(vanished, contents, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink("target", vanished); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(keep, []byte("kept"), 0600); err != nil {
				t.Fatal(err)
			}

			var logs, archive bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			options := archiveOptions{skipMissingFiles: true, sourceRoot: dir, logger: logger}
			operations := archiveFileOperations{lstat: os.Lstat, readlink: os.Readlink, open: os.Open}
			if kind != "symlink" {
				operations.open = func(path string) (*os.File, error) {
					if path == vanished {
						return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
					}
					return os.Open(path)
				}
			} else {
				operations.readlink = func(path string) (string, error) {
					return "", &os.PathError{Op: "readlink", Path: path, Err: os.ErrNotExist}
				}
			}
			writer := tar.NewWriter(&archive)
			if err := writeTarball(vanished, writer, dir, options, operations); err != nil {
				t.Fatalf("skip vanished entry: %v", err)
			}
			if err := writeTarball(keep, writer, dir, options, operations); err != nil {
				t.Fatalf("write surviving file: %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close archive: %v", err)
			}
			if !strings.Contains(logs.String(), vanished) || !strings.Contains(logs.String(), "WARN") {
				t.Fatalf("expected warning naming skipped entry, got %q", logs.String())
			}
			reader := tar.NewReader(&archive)
			header, err := reader.Next()
			if err != nil || filepath.Base(header.Name) != "keep.txt" {
				t.Fatalf("expected surviving entry, header = %v, error = %v", header, err)
			}
			content, err := io.ReadAll(reader)
			if err != nil || string(content) != "kept" {
				t.Fatalf("archived contents = %q, error = %v", content, err)
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatalf("expected end of archive, got %v", err)
			}
		})
	}
}

func TestMissingSourceRootStillFailsWhenSkippingEnabled(t *testing.T) {
	dir := t.TempDir()
	missingRoot := filepath.Join(dir, "missing-root")
	var logs bytes.Buffer
	options := archiveOptions{skipMissingFiles: true, logger: slog.New(slog.NewTextHandler(&logs, nil))}
	err := createArchive([]string{missingRoot}, missingRoot, filepath.Join(dir, "backup.tar"), "none", 1, options)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source root: got %v, want os.ErrNotExist", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("missing source root must not be skipped: %q", logs.String())
	}
}

func TestMissingSourceRootBeforeWalkStillFails(t *testing.T) {
	dir := t.TempDir()
	missingRoot := filepath.Join(dir, "missing-root")
	archive := filepath.Join(dir, "backup.tar")
	var logs bytes.Buffer
	s := &script{
		c:      &Config{BackupSources: missingRoot, BackupSkipMissingFiles: true},
		file:   archive,
		logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}
	if err := s.createArchive(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("walk missing source root: got %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(archive); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected archive for missing root: %v", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("missing source root must not be skipped: %q", logs.String())
	}
}

func TestOtherSourceErrorsStillFailWhenSkippingEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"lstat", "open"} {
		t.Run(operation, func(t *testing.T) {
			var logs, archive bytes.Buffer
			options := archiveOptions{skipMissingFiles: true, sourceRoot: dir, logger: slog.New(slog.NewTextHandler(&logs, nil))}
			operations := archiveFileOperations{lstat: os.Lstat, readlink: os.Readlink, open: os.Open}
			if operation == "lstat" {
				operations.lstat = func(path string) (os.FileInfo, error) {
					return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrPermission}
				}
			} else {
				operations.open = func(path string) (*os.File, error) {
					return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
				}
			}
			err := writeTarball(path, tar.NewWriter(&archive), dir, options, operations)
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("permission error: got %v, want os.ErrPermission", err)
			}
			if logs.Len() != 0 {
				t.Fatalf("permission error must not be skipped: %q", logs.String())
			}
		})
	}
}

type missingOutputWriter struct{}

func (missingOutputWriter) Write([]byte) (int, error) {
	return 0, os.ErrNotExist
}

func TestMissingArchiveOutputStillFailsWhenSkippingEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	options := archiveOptions{skipMissingFiles: true, sourceRoot: dir, logger: slog.New(slog.NewTextHandler(&logs, nil))}
	operations := archiveFileOperations{lstat: os.Lstat, readlink: os.Readlink, open: os.Open}
	err := writeTarball(path, tar.NewWriter(missingOutputWriter{}), dir, options, operations)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing archive output: got %v, want os.ErrNotExist", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("archive output error must not be skipped: %q", logs.String())
	}
}

type archiveOutputCloseError struct {
	bytes.Buffer
}

func (w *archiveOutputCloseError) Close() error {
	return os.ErrPermission
}

func TestArchiveOutputFileCloseError(t *testing.T) {
	for _, compression := range []string{"none", "gz", "zst"} {
		t.Run(compression, func(t *testing.T) {
			output := &archiveOutputCloseError{}
			operations := defaultArchiveFileOperations()
			operations.create = func(string) (io.WriteCloser, error) { return output, nil }
			var logs bytes.Buffer
			options := archiveOptions{skipMissingFiles: true, logger: slog.New(slog.NewTextHandler(&logs, nil)), operations: operations}
			err := compress(nil, filepath.Join(t.TempDir(), "backup.tar"), compression, 1, options)
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("archive output close: got %v, want os.ErrPermission", err)
			}
			var input io.Reader = bytes.NewReader(output.Bytes())
			switch compression {
			case "gz":
				gz, err := gzip.NewReader(input)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = gz.Close() })
				input = gz
			case "zst":
				z, err := zstd.NewReader(input)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(z.Close)
				input = z
			}
			if _, err := tar.NewReader(input).Next(); err != io.EOF {
				t.Fatalf("expected a complete empty archive before close, got %v", err)
			}
			if logs.Len() != 0 {
				t.Fatalf("output close error must not be skipped: %q", logs.String())
			}
		})
	}
}
