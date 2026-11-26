// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestMissingEntryBeforeHeaderLeavesValidArchive(t *testing.T) {
	for _, kind := range []string{"file", "empty file", "symlink"} {
		for _, compression := range []string{"none", "gz", "zst"} {
			t.Run(kind+"/"+compression, func(t *testing.T) {
				dir := t.TempDir()
				missing := filepath.Join(dir, "missing")
				keep := filepath.Join(dir, "keep.txt")
				if kind == "symlink" {
					if err := os.Symlink("target", missing); err != nil {
						t.Fatal(err)
					}
				} else {
					content := []byte("missing")
					if kind == "empty file" {
						content = nil
					}
					if err := os.WriteFile(missing, content, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(keep, []byte("kept"), 0600); err != nil {
					t.Fatal(err)
				}

				operations := defaultArchiveFileOperations()
				if kind == "symlink" {
					operations.readlink = func(path string) (string, error) {
						return "", &os.PathError{Op: "readlink", Path: path, Err: os.ErrNotExist}
					}
				} else {
					operations.open = func(path string) (*os.File, error) {
						if path == missing {
							return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
						}
						return os.Open(path)
					}
				}
				archive := filepath.Join(dir, "backup.tar")
				warnings, err := compress([]string{missing, keep}, archive, compression, 1, false, operations)
				if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0].Error(), missing) {
					t.Fatalf("warnings=%v error=%v", warnings, err)
				}
				file, err := os.Open(archive)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = file.Close() })
				var input io.Reader = file
				switch compression {
				case "gz":
					gz, err := gzip.NewReader(file)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = gz.Close() })
					input = gz
				case "zst":
					z, err := zstd.NewReader(file)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(z.Close)
					input = z
				}
				reader := tar.NewReader(input)
				header, err := reader.Next()
				if err != nil || filepath.Base(header.Name) != "keep.txt" {
					t.Fatalf("expected surviving entry, header=%v error=%v", header, err)
				}
				content, err := io.ReadAll(reader)
				if err != nil || string(content) != "kept" {
					t.Fatalf("archived contents=%q error=%v", content, err)
				}
				if _, err := reader.Next(); err != io.EOF {
					t.Fatalf("expected end of archive, got %v", err)
				}
			})
		}
	}
}

func TestOtherSourceErrorsStillFail(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"lstat", "open"} {
		t.Run(operation, func(t *testing.T) {
			operations := defaultArchiveFileOperations()
			if operation == "lstat" {
				operations.lstat = func(path string) (os.FileInfo, error) {
					return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrPermission}
				}
			} else {
				operations.open = func(path string) (*os.File, error) {
					return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
				}
			}
			warnings, err := compress([]string{file}, filepath.Join(t.TempDir(), "backup.tar"), "none", 1, false, operations)
			if !errors.Is(err, os.ErrPermission) || len(warnings) != 0 {
				t.Fatalf("permission error: warnings=%v error=%v", warnings, err)
			}
		})
	}
}

type missingOutputWriter struct{}

func (missingOutputWriter) Write([]byte) (int, error) { return 0, os.ErrNotExist }
func (missingOutputWriter) Close() error              { return nil }

func TestMissingArchiveOutputStillFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	operations := defaultArchiveFileOperations()
	operations.create = func(string) (io.WriteCloser, error) { return missingOutputWriter{}, nil }
	warnings, err := compress([]string{file}, filepath.Join(dir, "backup.tar"), "none", 1, false, operations)
	if !errors.Is(err, os.ErrNotExist) || len(warnings) != 0 {
		t.Fatalf("output error: warnings=%v error=%v", warnings, err)
	}
}

type archiveOutputCloseError struct{ bytes.Buffer }

func (w *archiveOutputCloseError) Close() error { return os.ErrPermission }

func TestArchiveOutputFileCloseError(t *testing.T) {
	for _, compression := range []string{"none", "gz", "zst"} {
		t.Run(compression, func(t *testing.T) {
			output := &archiveOutputCloseError{}
			operations := defaultArchiveFileOperations()
			operations.create = func(string) (io.WriteCloser, error) { return output, nil }
			warnings, err := compress(nil, filepath.Join(t.TempDir(), "backup.tar"), compression, 1, false, operations)
			if !errors.Is(err, os.ErrPermission) || len(warnings) != 0 {
				t.Fatalf("output close: warnings=%v error=%v", warnings, err)
			}
		})
	}
}

func TestWarningsAreReturnedBeforeLaterFatalError(t *testing.T) {
	dir := t.TempDir()
	missing, denied := filepath.Join(dir, "missing"), filepath.Join(dir, "denied")
	operations := defaultArchiveFileOperations()
	operations.lstat = func(path string) (os.FileInfo, error) {
		if path == denied {
			return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrPermission}
		}
		return os.Lstat(path)
	}
	warnings, err := compress([]string{missing, denied}, filepath.Join(dir, "backup.tar"), "none", 1, false, operations)
	if !errors.Is(err, os.ErrPermission) || len(warnings) != 1 || !strings.Contains(warnings[0].Error(), missing) {
		t.Fatalf("warnings=%v error=%v", warnings, err)
	}
}
