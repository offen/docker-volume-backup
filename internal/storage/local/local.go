package local

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	strg "github.com/offen/docker-volume-backup/internal/storage"
	t "github.com/offen/docker-volume-backup/internal/types"
	u "github.com/offen/docker-volume-backup/internal/utilities"
	"github.com/sirupsen/logrus"
)

type LocalStorage struct {
	*strg.StorageBackend
}

// Specific init procedure for the local storage provider.
func InitLocal(c *t.Config, l *logrus.Logger, s *t.Stats) *strg.StorageBackend {
	a := &strg.StorageBackend{
		Storage: &LocalStorage{},
		Name:    "Local",
		Logger:  l,
		Config:  c,
		Stats:   s,
	}
	r := &LocalStorage{a}
	a.Storage = r
	return a
}

// Specific copy function for the local storage provider.
func (stg *LocalStorage) Copy(file string) error {
	_, name := path.Split(file)

	if err := u.CopyFile(file, path.Join(stg.Config.BackupArchive, name)); err != nil {
		return fmt.Errorf("copyBackup: error copying file to local archive: %w", err)
	}
	stg.Logger.Infof("Stored copy of backup `%s` in local archive `%s`.", file, stg.Config.BackupArchive)

	if stg.Config.BackupLatestSymlink != "" {
		symlink := path.Join(stg.Config.BackupArchive, stg.Config.BackupLatestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			os.Remove(symlink)
		}
		if err := os.Symlink(name, symlink); err != nil {
			return fmt.Errorf("copyBackup: error creating latest symlink: %w", err)
		}
		stg.Logger.Infof("Created/Updated symlink `%s` for latest backup.", stg.Config.BackupLatestSymlink)
	}

	return nil
}

// Specific prune function for the local storage provider.
func (stg *LocalStorage) Prune(deadline time.Time) error {
	globPattern := path.Join(
		stg.Config.BackupArchive,
		fmt.Sprintf("%s*", stg.Config.BackupPruningPrefix),
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

	stg.Stats.Storages.Local = t.StorageStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	stg.DoPrune(len(matches), len(candidates), "local backup(s)", func() error {
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

	return nil
}
