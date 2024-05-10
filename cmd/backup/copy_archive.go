// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"path"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
	"golang.org/x/sync/errgroup"
)

// copyArchive makes sure the backup file is copied to both local and remote locations
// as per the given configuration.
func (s *script) copyArchive() error {
	_, name := path.Split(s.file)
	if stat, err := os.Stat(s.file); err != nil {
		return errwrap.Wrap(err, "unable to stat backup file")
	} else {
		size := stat.Size()
		s.stats.BackupFile = BackupFileStats{
			Size:     uint64(size),
			Name:     name,
			FullPath: s.file,
		}
	}

	eg := errgroup.Group{}
	for _, backend := range s.storages {
		b := backend
		eg.Go(func() error {
			return b.Copy(s.file)
		})
	}
	if err := eg.Wait(); err != nil {
		return errwrap.Wrap(err, "error copying archive")
	}

	return nil
}
