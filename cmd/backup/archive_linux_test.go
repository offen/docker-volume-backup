//go:build linux

// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type truncateSourceOnHeader struct {
	path      string
	truncated bool
	output    bytes.Buffer
}

func (w *truncateSourceOnHeader) Write(p []byte) (int, error) {
	if !w.truncated {
		if err := os.Truncate(w.path, 0); err != nil {
			return 0, err
		}
		w.truncated = true
	}
	return w.output.Write(p)
}

func TestTruncatedSourceFailsWhenClosingTar(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	output := &truncateSourceOnHeader{path: file}
	writer := tar.NewWriter(output)
	if err := writeTarball(file, writer, dir); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err == nil || !strings.Contains(err.Error(), "missed writing 4 bytes") {
		t.Fatalf("truncated source close: %v", err)
	}
}

func TestOutputSizeLimitFailsAfterMissingEntryWarning(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	file := filepath.Join(dir, "source")
	data := make([]byte, 65536)
	value := uint32(1)
	for i := range data {
		value = value*1664525 + 1013904223
		data[i] = byte(value >> 24)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	original := sha256.Sum256(data)

	for _, compression := range []string{"none", "gz", "zst"} {
		t.Run(compression, func(t *testing.T) {
			var originalLimit syscall.Rlimit
			if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &originalLimit); err != nil {
				t.Fatal(err)
			}
			if originalLimit.Cur < 1024 {
				t.Skip("the existing file-size limit is below the test limit")
			}
			limit := originalLimit
			limit.Cur = 1024
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &originalLimit); err != nil {
					t.Errorf("restore file-size limit: %v", err)
				}
			}()

			warnings, err := compress([]string{missing, file}, filepath.Join(dir, compression+".tar"), compression, 1, false)
			if len(warnings) != 1 || !errors.Is(warnings[0], os.ErrNotExist) || !errors.Is(err, syscall.EFBIG) {
				t.Fatalf("warnings=%v error=%v", warnings, err)
			}
			after, err := os.ReadFile(file)
			if err != nil || sha256.Sum256(after) != original {
				t.Fatalf("source data changed: %v", err)
			}
		})
	}
}
