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
	s *script
}

func newLocalhelper(s *script) *LocalHelper {
	a := &AbstractHelper{}
	r := &LocalHelper{a, s}
	a.Helper = r
	return r
}

func (helper *LocalHelper) copyArchive(name string) error {
	if err := copyFile(helper.s.file, path.Join(helper.s.c.BackupArchive, name)); err != nil {
		return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
	}
	helper.s.logger.Infof("Stored copy of backup `%s` in local archive `%s`.", helper.s.file, helper.s.c.BackupArchive)
	if helper.s.c.BackupLatestSymlink != "" {
		symlink := path.Join(helper.s.c.BackupArchive, helper.s.c.BackupLatestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			os.Remove(symlink)
		}
		if err := os.Symlink(name, symlink); err != nil {
			return fmt.Errorf("copyBackup: error creating latest symlink: %w", err)
		}
		helper.s.logger.Infof("Created/Updated symlink `%s` for latest backup.", helper.s.c.BackupLatestSymlink)
	}

	return nil
}

func (helper *LocalHelper) pruneBackups(deadline time.Time) error {
	globPattern := path.Join(
		helper.s.c.BackupArchive,
		fmt.Sprintf("%s*", helper.s.c.BackupPruningPrefix),
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

	helper.s.stats.Storages.Local = StorageStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	doPrune(helper.s, len(matches), len(candidates), "local backup(s)", func() error {
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
