// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

var noop = func() error { return nil }

// lock opens a lockfile at the given location, keeping it locked until the
// caller invokes the returned release func. In case the lock is currently blocked
// by another execution, it will repeatedly retry until the lock is available
// or the given timeout is exceeded.
func lock(lockfile string, timeout time.Duration) (func() error, error) {
	deadline := time.NewTimer(timeout)
	retry := time.NewTicker(5 * time.Second)
	fileLock := flock.New(lockfile)

	for {
		acquired, err := fileLock.TryLock()
		if err != nil {
			return noop, fmt.Errorf("lock: error trying lock: %w", err)
		}
		if acquired {
			return fileLock.Unlock, nil
		}
		select {
		case <-retry.C:
			continue
		case <-deadline.C:
			return noop, errors.New("lock: timed out waiting for lockfile to become available")
		}
	}
}

// copy creates a copy of the file located at `dst` at `src`.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// join takes a list of errors and joins them into a single error
func join(errs ...error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	var msgs []string
	for _, err := range errs {
		if err == nil {
			continue
		}
		msgs = append(msgs, err.Error())
	}
	return errors.New("[" + strings.Join(msgs, ", ") + "]")
}

// remove removes the given file or directory from disk.
func remove(location string) error {
	fi, err := os.Lstat(location)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove: error checking for existence of `%s`: %w", location, err)
	}
	if fi.IsDir() {
		err = os.RemoveAll(location)
	} else {
		err = os.Remove(location)
	}
	if err != nil {
		return fmt.Errorf("remove: error removing `%s`: %w", location, err)
	}
	return nil
}

// buffer takes an io.Writer and returns a wrapped version of the
// writer that writes to both the original target as well as the returned buffer
func buffer(w io.Writer) (io.Writer, *bytes.Buffer) {
	buffering := &bufferingWriter{buf: bytes.Buffer{}, writer: w}
	return buffering, &buffering.buf
}

type bufferingWriter struct {
	buf    bytes.Buffer
	writer io.Writer
}

func (b *bufferingWriter) Write(p []byte) (n int, err error) {
	if n, err := b.buf.Write(p); err != nil {
		return n, fmt.Errorf("bufferingWriter: error writing to buffer: %w", err)
	}
	return b.writer.Write(p)
}
