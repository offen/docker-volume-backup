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

// Specific init procedure for the local storage provider.
func InitLocal(c *t.Config, l *logrus.Logger) *LocalStorage {
	a := &GenericStorage{&LocalStorage{}, l, c}
	r := &LocalStorage{a}
	return r
}

// Specific copy function for the local storage provider.
func (stg *LocalStorage) copy(file string) error {
	_, name := path.Split(file)

	if err := u.CopyFile(file, path.Join(stg.config.BackupArchive, name)); err != nil {
		return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
	}
	stg.logger.Infof("Stored copy of backup `%s` in local archive `%s`.", file, stg.config.BackupArchive)

	if stg.config.BackupLatestSymlink != "" {
		symlink := path.Join(stg.config.BackupArchive, stg.config.BackupLatestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			os.Remove(symlink)
		}
		if err := os.Symlink(name, symlink); err != nil {
			return fmt.Errorf("copyBackup: error creating latest symlink: %w", err)
		}
		stg.logger.Infof("Created/Updated symlink `%s` for latest backup.", stg.config.BackupLatestSymlink)
	}

	return nil
}

// Specific prune function for the local storage provider.
func (stg *LocalStorage) prune(deadline time.Time) (*t.StorageStats, error) {
	globPattern := path.Join(
		stg.config.BackupArchive,
		fmt.Sprintf("%s*", stg.config.BackupPruningPrefix),
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

	stg.doPrune(len(matches), len(candidates), "local backup(s)", func() error {
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
