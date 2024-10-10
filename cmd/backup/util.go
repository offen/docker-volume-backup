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
	"strings"
	
	"github.com/offen/docker-volume-backup/internal/errwrap"
	"github.com/robfig/cron/v3"
	cron_explain "github.com/lnquy/cron"
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

// explainCronExpression describes the cron expression in plain text.
func explainCronExpression(expression string, locale string) (description string, ok bool) {
    predefinedMap := map[string]string{
        "@yearly":  "0 0 1 1 *",
        "@annually": "0 0 1 1 *",
        "@monthly": "0 0 1 * *",
        "@weekly":  "0 0 * * 0",
        "@daily":   "0 0 * * *",
        "@hourly":  "0 * * * *",
    }

    if translated, exists := predefinedMap[expression]; exists {
        expression = translated
    }

    localeMap := map[string]cron_explain.LocaleType{
        "cs": cron_explain.Locale_cs,
        "da": cron_explain.Locale_da,
        "de": cron_explain.Locale_de,
        "en": cron_explain.Locale_en,
        "es": cron_explain.Locale_es,
        "fa": cron_explain.Locale_fa,
        "fi": cron_explain.Locale_fi,
        "fr": cron_explain.Locale_fr,
        "he": cron_explain.Locale_he,
        "it": cron_explain.Locale_it,
        "ja": cron_explain.Locale_ja,
        "ko": cron_explain.Locale_ko,
        "nb": cron_explain.Locale_nb,
        "nl": cron_explain.Locale_nl,
        "pl": cron_explain.Locale_pl,
        "pt_BR": cron_explain.Locale_pt_BR,
        "ro": cron_explain.Locale_ro,
        "ru": cron_explain.Locale_ru,
        "sk": cron_explain.Locale_sk,
        "sl": cron_explain.Locale_sl,
        "sv": cron_explain.Locale_sv,
        "sw": cron_explain.Locale_sw,
        "tr": cron_explain.Locale_tr,
        "uk": cron_explain.Locale_uk,
        "zh_CN": cron_explain.Locale_zh_CN,
        "zh_TW": cron_explain.Locale_zh_TW,
    }

    selectedLocale, exists := localeMap[locale]
    if !exists {
        selectedLocale = cron_explain.Locale_en
    }

    exprDesc, _ := cron_explain.NewDescriptor()
    desc, err := exprDesc.ToDescription(expression, selectedLocale)
    if err != nil {
        return "", false
    }

    // Ensure the first letter of the description is lowercase
    if len(desc) > 0 {
        desc = strings.ToLower(string(desc[0])) + desc[1:]
    }

    return desc, true
}
