package storages

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	t "github.com/offen/docker-volume-backup/cmd/backup/types"
	u "github.com/offen/docker-volume-backup/cmd/backup/utilities"
	"github.com/sirupsen/logrus"
)

type LocalStorage struct {
	*GenericStorage
}

func InitLocal(c *t.Config, l *logrus.Logger) *LocalStorage {
	a := &GenericStorage{}
	r := &LocalStorage{a}
	a.backupRetentionDays = c.BackupRetentionDays
	a.backupPruningPrefix = c.BackupPruningPrefix
	a.logger = l
	a.config = c
	a.Storage = r
	return r
}

func (lc *LocalStorage) Copy(file string) error {
	_, name := path.Split(file)

	if err := u.CopyFile(file, path.Join(lc.config.BackupArchive, name)); err != nil {
		return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
	}
	lc.logger.Infof("Stored copy of backup `%s` in local archive `%s`.", file, lc.config.BackupArchive)

	if lc.config.BackupLatestSymlink != "" {
		symlink := path.Join(lc.config.BackupArchive, lc.config.BackupLatestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			os.Remove(symlink)
		}
		if err := os.Symlink(name, symlink); err != nil {
			return fmt.Errorf("copyBackup: error creating latest symlink: %w", err)
		}
		lc.logger.Infof("Created/Updated symlink `%s` for latest backup.", lc.config.BackupLatestSymlink)
	}

	return nil
}

func (lc *LocalStorage) Prune(deadline time.Time) (*t.StorageStats, error) {
	globPattern := path.Join(
		lc.config.BackupArchive,
		fmt.Sprintf("%s*", lc.backupPruningPrefix),
	)
	globMatches, err := filepath.Glob(globPattern)
	if err != nil {
		return nil, fmt.Errorf(
			"pruneBackups: error looking up matching files using pattern %s: %w",
			globPattern,
			err,
		)
	}

	var candidates []string
	for _, candidate := range globMatches {
		fi, err := os.Lstat(candidate)
		if err != nil {
			return nil, fmt.Errorf(
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
			return nil, fmt.Errorf(
				"pruneBackups: error calling stat on file %s: %w",
				candidate,
				err,
			)
		}
		if fi.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stats := t.StorageStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	lc.doPrune(len(matches), len(candidates), "local backup(s)", func() error {
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
				u.Join(removeErrors...),
			)
		}
		return nil
	})

	return &stats, nil
}
