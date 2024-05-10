// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
	"github.com/otiai10/copy"
)

// createArchive creates a tar archive of the configured backup location and
// saves it to disk.
func (s *script) createArchive() error {
	backupSources := s.c.BackupSources

	if s.c.BackupFromSnapshot {
		s.logger.Warn(
			"Using BACKUP_FROM_SNAPSHOT has been deprecated and will be removed in the next major version.",
		)
		s.logger.Warn(
			"Please use `archive-pre` and `archive-post` commands to prepare your backup sources. Refer to the documentation for an upgrade guide.",
		)
		backupSources = filepath.Join("/tmp", s.c.BackupSources)
		// copy before compressing guard against a situation where backup folder's content are still growing.
		s.registerHook(hookLevelPlumbing, func(error) error {
			if err := remove(backupSources); err != nil {
				return errwrap.Wrap(err, "error removing snapshot")
			}
			s.logger.Info(
				fmt.Sprintf("Removed snapshot `%s`.", backupSources),
			)
			return nil
		})
		if err := copy.Copy(s.c.BackupSources, backupSources, copy.Options{
			PreserveTimes: true,
			PreserveOwner: true,
		}); err != nil {
			return errwrap.Wrap(err, "error creating snapshot")
		}
		s.logger.Info(
			fmt.Sprintf("Created snapshot of `%s` at `%s`.", s.c.BackupSources, backupSources),
		)
	}

	tarFile := s.file
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(tarFile); err != nil {
			return errwrap.Wrap(err, "error removing tar file")
		}
		s.logger.Info(
			fmt.Sprintf("Removed tar file `%s`.", tarFile),
		)
		return nil
	})

	backupPath, err := filepath.Abs(stripTrailingSlashes(backupSources))
	if err != nil {
		return errwrap.Wrap(err, "error getting absolute path")
	}

	var filesEligibleForBackup []string
	if err := filepath.WalkDir(backupPath, func(path string, di fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if s.c.BackupExcludeRegexp.Re != nil && s.c.BackupExcludeRegexp.Re.MatchString(path) {
			return nil
		}
		filesEligibleForBackup = append(filesEligibleForBackup, path)
		return nil
	}); err != nil {
		return errwrap.Wrap(err, "error walking filesystem tree")
	}

	if err := createArchive(filesEligibleForBackup, backupSources, tarFile, s.c.BackupCompression.String(), s.c.GzipParallelism.Int()); err != nil {
		return errwrap.Wrap(err, "error compressing backup folder")
	}

	s.logger.Info(
		fmt.Sprintf("Created backup of `%s` at `%s`.", backupSources, tarFile),
	)
	return nil
}
