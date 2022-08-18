// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"text/template"
	"time"

	sTypes "github.com/containrrr/shoutrrr/pkg/types"
	"github.com/offen/docker-volume-backup/internal/utilities"
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
		return fmt.Errorf("notifyFailure: error executing %s template: %w", titleTemplate, err)
	}

	bodyBuf := &bytes.Buffer{}
	if err := s.template.ExecuteTemplate(bodyBuf, bodyTemplate, params); err != nil {
		return fmt.Errorf("notifyFailure: error executing %s template: %w", bodyTemplate, err)
	}

	if err := s.sendNotification(titleBuf.String(), bodyBuf.String()); err != nil {
		return fmt.Errorf("notifyFailure: error notifying: %w", err)
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
		return fmt.Errorf("sendNotification: error sending message: %w", utilities.Join(errs...))
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
	"env": os.Getenv,
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
