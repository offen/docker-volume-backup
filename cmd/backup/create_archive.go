// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/offen/docker-volume-backup/internal/errwrap"
	"github.com/otiai10/copy"
)

// createArchive creates a tar archive of the configured backup location and
// saves it to disk.
func (s *script) createArchive() error {
	operations := defaultArchiveFileOperations()
	if s.archiveLstat != nil {
		operations.lstat = s.archiveLstat
	}
	backupSources := s.c.BackupSources

	if s.c.BackupFromSnapshot {
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
	var sourceRoot os.FileInfo
	var sourceDirectories map[string]os.FileInfo
	if s.c.BackupSkipMissingFiles {
		sourceRoot, err = operations.lstat(backupPath)
		if err != nil {
			return errwrap.Wrap(err, "error getting backup source root")
		}
		sourceDirectories = make(map[string]os.FileInfo)
		if sourceRoot.IsDir() {
			sourceDirectories[backupPath] = sourceRoot
		}
	}

	var filesEligibleForBackup []string
	if err := filepath.WalkDir(backupPath, func(path string, di fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if s.c.BackupExcludeRegexp.Re != nil && s.c.BackupExcludeRegexp.Re.MatchString(path) {
			return nil
		}
		if sourceDirectories != nil && path != backupPath {
			if di.IsDir() {
				info, err := di.Info()
				if err != nil {
					return err
				}
				if !info.IsDir() {
					return errwrap.Wrap(nil, fmt.Sprintf("backup source directory changed while listing: %s", path))
				}
				sourceDirectories[path] = info
			}
			for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
				if _, tracked := sourceDirectories[parent]; tracked {
					break
				}
				info, err := os.Lstat(parent)
				if err != nil {
					return err
				}
				if !info.IsDir() {
					return errwrap.Wrap(nil, fmt.Sprintf("backup source directory changed while listing: %s", parent))
				}
				sourceDirectories[parent] = info
				if parent == backupPath || parent == filepath.Dir(parent) {
					break
				}
			}
		}
		filesEligibleForBackup = append(filesEligibleForBackup, path)
		return nil
	}); err != nil {
		return errwrap.Wrap(err, "error walking filesystem tree")
	}

	options := archiveOptions{
		skipMissingFiles:  s.c.BackupSkipMissingFiles,
		sourceRootInfo:    sourceRoot,
		sourceDirectories: sourceDirectories,
		logger:            s.logger,
		operations:        operations,
	}
	if err := createArchive(filesEligibleForBackup, backupSources, tarFile, s.c.BackupCompression.String(), s.c.GzipParallelism.Int(), options); err != nil {
		return errwrap.Wrap(err, "error compressing backup folder")
	}
	if s.c.BackupSkipMissingFiles {
		currentRoot, err := operations.lstat(backupPath)
		if err != nil {
			return errwrap.Wrap(err, "error verifying backup source root")
		}
		if !os.SameFile(sourceRoot, currentRoot) {
			return errwrap.Wrap(nil, "backup source root changed while creating archive")
		}
		for path, original := range sourceDirectories {
			if path == backupPath {
				continue
			}
			current, err := operations.lstat(path)
			if err != nil {
				return errwrap.Wrap(err, fmt.Sprintf("error verifying backup source directory %s", path))
			}
			if !os.SameFile(original, current) {
				return errwrap.Wrap(nil, fmt.Sprintf("backup source directory changed while creating archive: %s", path))
			}
		}
	}

	s.logger.Info(
		fmt.Sprintf("Created backup of `%s` at `%s`.", backupSources, tarFile),
	)
	return nil
}
