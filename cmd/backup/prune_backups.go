// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
	"golang.org/x/sync/errgroup"
)

// pruneBackups rotates away backups from local and remote storages using
// the given configuration. In case the given configuration would delete all
// backups, it does nothing instead and logs a warning.
func (s *script) pruneBackups() error {
	if s.c.BackupRetentionDays < 0 {
		return nil
	}

	deadline := time.Now().AddDate(0, 0, -int(s.c.BackupRetentionDays)).Add(s.c.BackupPruningLeeway)

	eg := errgroup.Group{}
	for _, backend := range s.storages {
		b := backend
		eg.Go(func() error {
			if skipPrune(b.Name(), s.c.BackupSkipBackendsFromPrune) {
				s.logger.Info(
					fmt.Sprintf("Skipping pruning for backend `%s`.", b.Name()),
				)
				return nil
			}
			stats, err := b.Prune(deadline, s.c.BackupPruningPrefix)
			if err != nil {
				return err
			}
			s.stats.Lock()
			s.stats.Storages[b.Name()] = StorageStats{
				Total:  stats.Total,
				Pruned: stats.Pruned,
			}
			s.stats.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return errwrap.Wrap(err, "error pruning backups")
	}

	return nil
}

// skipPrune returns true if the given backend name is contained in the
// list of skipped backends.
func skipPrune(name string, skippedBackends []string) bool {
	return slices.ContainsFunc(
		skippedBackends,
		func(b string) bool {
			return strings.EqualFold(b, name) // ignore case on both sides
		},
	)
}
