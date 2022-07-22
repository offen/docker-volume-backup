// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package utilites

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var Noop = func() error { return nil }

// copy creates a copy of the file located at `dst` at `src`.
func CopyFile(src, dst string) error {
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
func Join(errs ...error) error {
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
func Remove(location string) error {
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
func Buffer(w io.Writer) (io.Writer, *bytes.Buffer) {
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
