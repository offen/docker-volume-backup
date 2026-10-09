// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"testing"
	"time"

	"github.com/offen/envconfig"
)

func TestSSHUploadRetryConfiguration(t *testing.T) {
	originalLookup := envconfig.Lookup
	t.Cleanup(func() { envconfig.Lookup = originalLookup })
	for _, test := range []struct {
		name    string
		values  map[string]string
		retries int
		delay   time.Duration
		invalid bool
	}{
		{name: "defaults", retries: 2, delay: 5 * time.Second},
		{name: "disabled", values: map[string]string{"SSH_UPLOAD_RETRIES": "0"}, delay: 5 * time.Second},
		{name: "custom", values: map[string]string{"SSH_UPLOAD_RETRIES": "4", "SSH_UPLOAD_RETRY_DELAY": "500ms"}, retries: 4, delay: 500 * time.Millisecond},
		{name: "negative retries", values: map[string]string{"SSH_UPLOAD_RETRIES": "-1"}, invalid: true},
		{name: "invalid delay", values: map[string]string{"SSH_UPLOAD_RETRY_DELAY": "invalid"}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadConfig(func(key string) (string, bool) {
				value, ok := test.values[key]
				return value, ok
			})
			if test.invalid {
				if err == nil {
					t.Fatal("Expected invalid retry configuration to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if int(config.SSHUploadRetries) != test.retries || config.SSHUploadRetryDelay != test.delay {
				t.Errorf("Got retries=%d, delay=%s; want %d, %s", config.SSHUploadRetries, config.SSHUploadRetryDelay, test.retries, test.delay)
			}
		})
	}
}
