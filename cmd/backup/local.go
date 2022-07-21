package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"
)

type LocalHelper struct {
	*AbstractHelper
}

func newLocalhelper() *LocalHelper {
	a := &AbstractHelper{}
	r := &LocalHelper{a}
	a.Helper = r
	return r
}

func (helper *LocalHelper) copyArchive(s *script, name string) error {
	if err := copyFile(s.file, path.Join(s.c.BackupArchive, name)); err != nil {
		return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
	}
	s.logger.Infof("Stored copy of backup `%s` in local archive `%s`.", s.file, s.c.BackupArchive)
	if s.c.BackupLatestSymlink != "" {
		symlink := path.Join(s.c.BackupArchive, s.c.BackupLatestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			os.Remove(symlink)
		}
		if err := os.Symlink(name, symlink); err != nil {
			return fmt.Errorf("copyBackup: error creating latest symlink: %w", err)
		}
		s.logger.Infof("Created/Updated symlink `%s` for latest backup.", s.c.BackupLatestSymlink)
	}

	return nil
}

func (helper *LocalHelper) pruneBackups(s *script, deadline time.Time) error {
	globPattern := path.Join(
		s.c.BackupArchive,
		fmt.Sprintf("%s*", s.c.BackupPruningPrefix),
	)
	globMatches, err := filepath.Glob(globPattern)
	if err != nil {
		return fmt.Errorf(
			"pruneBackups: error looking up matching files using pattern %s: %w",
			globPattern,
			err,
		)
	}

	var candidates []string
	for _, candidate := range globMatches {
		fi, err := os.Lstat(candidate)
		if err != nil {
			return fmt.Errorf(
				"pruneBackups: error calling Lstat on file %s: %w",
				candidate,
				err,
			)
		}

		if fi.Mode()&os.ModeSymlink != os.ModeSymlink {
			candidates = append(candidates, candidate)
		}
	}

	var matches []string
	for _, candidate := range candidates {
		fi, err := os.Stat(candidate)
		if err != nil {
			return fmt.Errorf(
				"pruneBackups: error calling stat on file %s: %w",
				candidate,
				err,
			)
		}
		if fi.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	s.stats.Storages.Local = StorageStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	doPrune(s, len(matches), len(candidates), "local backup(s)", func() error {
		var removeErrors []error
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				removeErrors = append(removeErrors, err)
			}
		}
		if len(removeErrors) != 0 {
			return fmt.Errorf(
				"pruneBackups: %d error(s) deleting local files, starting with: %w",
				len(removeErrors),
				join(removeErrors...),
			)
		}
		return nil
	})

	return nil
}
