// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"testing"
	"time"
)

func TestJitterDelay(t *testing.T) {
	tests := []struct {
		name      string
		max       time.Duration
		scheduled bool
		expectNil bool
	}{
		{"manual run ignores jitter", time.Hour, false, true},
		{"scheduled run without jitter", 0, true, true},
		{"scheduled run with negative jitter", -time.Minute, true, true},
		{"scheduled run with jitter", time.Hour, true, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// the delay is random, so repeat to make sure it never leaves its bounds
			for i := 0; i < 1000; i++ {
				delay := jitterDelay(test.max, test.scheduled)
				if test.expectNil {
					if delay != 0 {
						t.Fatalf("Expected no delay, got %v", delay)
					}
					continue
				}
				if delay < 0 || delay > test.max {
					t.Fatalf("Expected delay between 0 and %v, got %v", test.max, delay)
				}
			}
		})
	}
}
