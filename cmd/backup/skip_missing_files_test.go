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

func TestCompressMissingFileFailsByDefault(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	archive := filepath.Join(dir, "backup.tar")

	err := compress([]string{missing}, archive, "none", 1, archiveOptions{})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("compress missing file: got %v, want os.ErrNotExist", err)
	}
}

func TestCompressSkipsMissingFileWhenEnabled(t *testing.T) {
	previousLookup := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = previousLookup })
	t.Setenv("BACKUP_SKIP_MISSING_FILES", "true")
	config, err := loadConfig(os.LookupEnv)
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}

	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	missing := filepath.Join(dir, "disappeared.txt")
	archive := filepath.Join(dir, "backup.tar")
	if err := os.WriteFile(keep, []byte("still present"), 0600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	options := archiveOptions{skipMissingFiles: config.BackupSkipMissingFiles, sourceRoot: dir, logger: logger}
	if err := compress([]string{missing, keep}, archive, "none", 1, options); err != nil {
		t.Fatalf("compress with skipping enabled: %v", err)
	}
	if !strings.Contains(logs.String(), missing) || !strings.Contains(logs.String(), "WARN") {
		t.Fatalf("expected warning naming skipped file, got %q", logs.String())
	}

	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	reader := tar.NewReader(f)
	header, err := reader.Next()
	if err != nil {
		t.Fatalf("read archived file: %v", err)
	}
	if filepath.Base(header.Name) != "keep.txt" {
		t.Fatalf("archived name = %q, want keep.txt", header.Name)
	}
	contents, err := io.ReadAll(reader)
	if err != nil || string(contents) != "still present" {
		t.Fatalf("archived contents = %q, error = %v", contents, err)
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("archive should end after retained file, got %v", err)
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

func TestMissingSourceRootDuringWalkStillFails(t *testing.T) {
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
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	contents := []byte("surviving bytes")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	for _, algo := range []string{"none", "gz", "zst"} {
		t.Run(algo, func(t *testing.T) {
			output := &archiveOutputCloseError{}
			err := compressFile(output, []string{path}, filepath.Join(dir, "backup.tar"), algo, 1, archiveOptions{})
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("archive output close: got %v, want os.ErrPermission", err)
			}
			var input io.Reader = bytes.NewReader(output.Bytes())
			switch algo {
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
			reader := tar.NewReader(input)
			header, err := reader.Next()
			if err != nil || filepath.Base(header.Name) != "file" {
				t.Fatalf("archived file: header=%v, error=%v", header, err)
			}
			archived, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(archived, contents) {
				t.Fatalf("archived contents=%q, error=%v", archived, err)
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatalf("expected end of archive, got %v", err)
			}
			source, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(source, contents) {
				t.Fatalf("source changed: contents=%q, error=%v", source, err)
			}
		})
	}
}
