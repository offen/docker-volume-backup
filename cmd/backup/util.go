// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
	"github.com/robfig/cron/v3"
)

var noop = func() error { return nil }

// remove removes the given file or directory from disk.
func remove(location string) error {
	fi, err := os.Lstat(location)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errwrap.Wrap(err, fmt.Sprintf("error checking for existence of `%s`", location))
	}
	if fi.IsDir() {
		err = os.RemoveAll(location)
	} else {
		err = os.Remove(location)
	}
	if err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error removing `%s", location))
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
		return n, errwrap.Wrap(err, "error writing to buffer")
	}
	return b.writer.Write(p)
}

type noopWriteCloser struct {
	io.Writer
}

func (noopWriteCloser) Close() error {
	return nil
}

type handledSwarmService struct {
	serviceID           string
	initialReplicaCount uint64
}

type concurrentSlice[T any] struct {
	val []T
	sync.Mutex
}

func (c *concurrentSlice[T]) append(v T) {
	c.Lock()
	defer c.Unlock()
	c.val = append(c.val, v)
}

func (c *concurrentSlice[T]) value() []T {
	return c.val
}

// checkCronSchedule detects whether the given cron expression will actually
// ever be executed or not.
func checkCronSchedule(expression string) (ok bool) {
	defer func() {
		if err := recover(); err != nil {
			ok = false
		}
	}()
	sched, err := cron.ParseStandard(expression)
	if err != nil {
		ok = false
		return
	}
	now := time.Now()
	sched.Next(now) // panics when the cron would never run
	ok = true
	return
}
