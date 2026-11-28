// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestMissingListedEntryLeavesValidArchive(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(missing, []byte("missing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{missing, keep}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(dir, "backup.tar")
	warnings, err := compress(paths, archive, "none", 1, false)
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0].Error(), missing) {
		t.Fatalf("warnings=%v error=%v", warnings, err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	reader := tar.NewReader(file)
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
}

type removeSourceOnHeader struct {
	path    string
	removed bool
	output  bytes.Buffer
}

func (w *removeSourceOnHeader) Write(p []byte) (int, error) {
	if !w.removed {
		if err := os.Remove(w.path); err != nil {
			return 0, err
		}
		w.removed = true
	}
	return w.output.Write(p)
}

func TestOpenFileBeforeWritingTarHeader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("removing an open file is not portable to Windows")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	output := &removeSourceOnHeader{path: file}
	writer := tar.NewWriter(output)
	if err := writeTarball(file, writer, dir); err != nil {
		t.Fatalf("write archive after removal: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !output.removed {
		t.Fatal("source was not removed during header write")
	}
	reader := tar.NewReader(&output.output)
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "kept" {
		t.Fatalf("archived contents=%q error=%v", content, err)
	}
}

func TestOtherSourceErrorsStillFail(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("filesystem permissions cannot be tested as root or on Windows")
	}
	for _, operation := range []string{"lstat", "open"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			sourceDir := filepath.Join(dir, "source")
			if err := os.Mkdir(sourceDir, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(sourceDir, "file")
			if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
				t.Fatal(err)
			}
			denied := file
			if operation == "lstat" {
				denied = sourceDir
			}
			if err := os.Chmod(denied, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(denied, 0700) })
			warnings, err := compress([]string{file}, filepath.Join(dir, "backup.tar"), "none", 1, false)
			if !errors.Is(err, os.ErrPermission) || len(warnings) != 0 {
				t.Fatalf("permission error: warnings=%v error=%v", warnings, err)
			}
		})
	}
}

type missingOutputWriter struct{}

func (missingOutputWriter) Write([]byte) (int, error) { return 0, os.ErrNotExist }

func TestMissingArchiveOutputStillFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	err := writeTarball(file, tar.NewWriter(missingOutputWriter{}), dir)
	var missing missingSourceError
	if !errors.Is(err, os.ErrNotExist) || errors.As(err, &missing) {
		t.Fatalf("output error must not be a skippable missing source: %v", err)
	}
}

func TestArchiveOutputWriteError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full is only available on Linux")
	}
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full is unavailable")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	warnings, err := compress([]string{file}, "/dev/full", "none", 1, false)
	if !errors.Is(err, syscall.ENOSPC) || len(warnings) != 0 {
		t.Fatalf("output write: warnings=%v error=%v", warnings, err)
	}
}

func TestWarningsAreReturnedBeforeLaterFatalError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("filesystem permissions cannot be tested as root or on Windows")
	}
	dir := t.TempDir()
	missing, denied := filepath.Join(dir, "missing"), filepath.Join(dir, "denied")
	if err := os.WriteFile(denied, []byte("denied"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(denied, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(denied, 0600) })
	warnings, err := compress([]string{missing, denied}, filepath.Join(dir, "backup.tar"), "none", 1, false)
	if !errors.Is(err, os.ErrPermission) || len(warnings) != 1 || !strings.Contains(warnings[0].Error(), missing) {
		t.Fatalf("warnings=%v error=%v", warnings, err)
	}
}
