// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/template"
	"time"

	sTypes "github.com/containrrr/shoutrrr/pkg/types"
	"github.com/jattento/docker-volume-backup/internal/errwrap"
)

//go:embed notifications.tmpl
var defaultNotifications string

// NotificationData data to be passed to the notification templates
type NotificationData struct {
	Error  error
	Config *Config
	Stats  *Stats
}

// notify sends a notification using the given title and body templates.
// Automatically creates notification data, adding the given error
func (s *script) notify(titleTemplate string, bodyTemplate string, err error) error {
	params := NotificationData{
		Error:  err,
		Stats:  s.stats,
		Config: s.c,
	}

	titleBuf := &bytes.Buffer{}
	if err := s.template.ExecuteTemplate(titleBuf, titleTemplate, params); err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error executing %s template", titleTemplate))
	}

	bodyBuf := &bytes.Buffer{}
	if err := s.template.ExecuteTemplate(bodyBuf, bodyTemplate, params); err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error executing %s template", bodyTemplate))
	}

	if err := s.sendNotification(titleBuf.String(), bodyBuf.String()); err != nil {
		return errwrap.Wrap(err, "error sending notification")
	}
	return nil
}

// notifyFailure sends a notification about a failed backup run
func (s *script) notifyFailure(err error) error {
	return s.notify("title_failure", "body_failure", err)
}

// notifyFailure sends a notification about a successful backup run
func (s *script) notifySuccess() error {
	return s.notify("title_success", "body_success", nil)
}

// sendNotification sends a notification to all configured third party services
func (s *script) sendNotification(title, body string) error {
	var errs []error
	for _, result := range s.sender.Send(body, &sTypes.Params{"title": title}) {
		if result != nil {
			errs = append(errs, result)
		}
	}
	if len(errs) != 0 {
		return errwrap.Wrap(errors.Join(errs...), "error sending message")
	}
	return nil
}

var templateHelpers = template.FuncMap{
	"formatTime": func(t time.Time) string {
		return t.Format(time.RFC3339)
	},
	"formatBytesDec": func(bytes uint64) string {
		return formatBytes(bytes, true)
	},
	"formatBytesBin": func(bytes uint64) string {
		return formatBytes(bytes, false)
	},
	"env":          os.Getenv,
	"toJson":       toJson,
	"toPrettyJson": toPrettyJson,
}

// formatBytes converts an amount of bytes in a human-readable representation
// the decimal parameter specifies if using powers of 1000 (decimal) or powers of 1024 (binary)
func formatBytes(b uint64, decimal bool) string {
	unit := uint64(1024)
	format := "%.1f %ciB"
	if decimal {
		unit = uint64(1000)
		format = "%.1f %cB"
	}
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf(format, float64(b)/float64(div), "kMGTPE"[exp])
}

func toJson(v interface{}) string {
	var bytes []byte
	var err error
	if bytes, err = json.Marshal(v); err != nil {
		return fmt.Sprintf("failed to marshal JSON in notification template: %v", err)
	}
	return string(bytes)
}

func toPrettyJson(v interface{}) string {
	var bytes []byte
	var err error
	if bytes, err = json.MarshalIndent(v, "", "  "); err != nil {
		return fmt.Sprintf("failed to marshal indent JSON in notification template: %v", err)
	}
	return string(bytes)
}
