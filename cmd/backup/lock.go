// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/flock"
)

// lock opens a lockfile at the given location, keeping it locked until the
// caller invokes the returned release func. In case the lock is currently blocked
// by another execution, it will repeatedly retry until the lock is available
// or the given timeout is exceeded.
func (s *script) lock(lockfile string) (func() error, error) {
	start := time.Now()
	defer func() {
		s.stats.LockedTime = time.Now().Sub(start)
	}()

	retry := time.NewTicker(5 * time.Second)
	defer retry.Stop()
	deadline := time.NewTimer(s.c.LockTimeout)
	defer deadline.Stop()

	fileLock := flock.New(lockfile)

	for {
		acquired, err := fileLock.TryLock()
		if err != nil {
			return noop, fmt.Errorf("lock: error trying lock: %w", err)
		}
		if acquired {
			if s.encounteredLock {
				s.logger.Info("Acquired exclusive lock on subsequent attempt, ready to continue.")
			}
			return fileLock.Unlock, nil
		}

		if !s.encounteredLock {
			s.logger.Infof(
				"Exclusive lock was not available on first attempt. Will retry until it becomes available or the timeout of %s is exceeded.",
				s.c.LockTimeout,
			)
			s.encounteredLock = true
		}

		select {
		case <-retry.C:
			continue
		case <-deadline.C:
			return noop, errors.New("lock: timed out waiting for lockfile to become available")
		}
	}
}
