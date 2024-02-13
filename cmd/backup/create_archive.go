// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io/fs"
	"path/filepath"

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
				return fmt.Errorf("createArchive: error removing snapshot: %w", err)
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
			return fmt.Errorf("createArchive: error creating snapshot: %w", err)
		}
		s.logger.Info(
			fmt.Sprintf("Created snapshot of `%s` at `%s`.", s.c.BackupSources, backupSources),
		)
	}

	tarFile := s.file
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(tarFile); err != nil {
			return fmt.Errorf("createArchive: error removing tar file: %w", err)
		}
		s.logger.Info(
			fmt.Sprintf("Removed tar file `%s`.", tarFile),
		)
		return nil
	})

	backupPath, err := filepath.Abs(stripTrailingSlashes(backupSources))
	if err != nil {
		return fmt.Errorf("createArchive: error getting absolute path: %w", err)
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
		return fmt.Errorf("createArchive: error walking filesystem tree: %w", err)
	}

	if err := createArchive(filesEligibleForBackup, backupSources, tarFile, s.c.BackupCompression.String(), s.c.GzipParallelism.Int()); err != nil {
		return fmt.Errorf("createArchive: error compressing backup folder: %w", err)
	}

	s.logger.Info(
		fmt.Sprintf("Created backup of `%s` at `%s`.", backupSources, tarFile),
	)
	return nil
}
