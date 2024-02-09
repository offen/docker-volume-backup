// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"time"

	"github.com/robfig/cron/v3"
)

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
