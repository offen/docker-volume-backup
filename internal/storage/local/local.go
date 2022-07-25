package local

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/offen/docker-volume-backup/internal/types"
	utilites "github.com/offen/docker-volume-backup/internal/utilities"
)

type localStorage struct {
	*storage.StorageBackend
	latestSymlink string
}

// NewStorageBackend creates and initializes a new local storage backend.
func NewStorageBackend(archivePath string, latestSymlink string, logFunc storage.LogFuncDef,
	s *types.Stats) storage.Backend {

	strgBackend := &storage.StorageBackend{
		Backend:         &localStorage{},
		Name:            "Local",
		DestinationPath: archivePath,
		Log:             logFunc,
		Stats:           s,
	}
	localBackend := &localStorage{
		StorageBackend: strgBackend,
		latestSymlink:  latestSymlink,
	}
	strgBackend.Backend = localBackend
	return strgBackend
}

// Copy copies the given file to the local storage backend.
func (stg *localStorage) Copy(file string) error {
	if _, err := os.Stat(stg.DestinationPath); os.IsNotExist(err) {
		return nil
	}

	_, name := path.Split(file)

	if err := utilites.CopyFile(file, path.Join(stg.DestinationPath, name)); err != nil {
		return stg.Log(storage.ERROR, stg.Name, "copyBackup: error copying file to local archive: %w", err)
	}
	stg.Log(storage.INFO, stg.Name, "Stored copy of backup `%s` in local archive `%s`.", file, stg.DestinationPath)

	if stg.latestSymlink != "" {
		symlink := path.Join(stg.DestinationPath, stg.latestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			os.Remove(symlink)
		}
		if err := os.Symlink(name, symlink); err != nil {
			return stg.Log(storage.ERROR, stg.Name, "Copy: error creating latest symlink! %w", err)
		}
		stg.Log(storage.INFO, stg.Name, "Created/Updated symlink `%s` for latest backup.", stg.latestSymlink)
	}

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the local storage backend.
func (stg *localStorage) Prune(deadline time.Time, pruningPrefix string) error {
	globPattern := path.Join(
		stg.DestinationPath,
		fmt.Sprintf("%s*", pruningPrefix),
	)
	globMatches, err := filepath.Glob(globPattern)
	if err != nil {
		return stg.Log(storage.ERROR, stg.Name,
			"Prune: Error looking up matching files using pattern %s! %w",
			globPattern,
			err,
		)
	}

	var candidates []string
	for _, candidate := range globMatches {
		fi, err := os.Lstat(candidate)
		if err != nil {
			return stg.Log(storage.ERROR, stg.Name,
				"Prune: Error calling Lstat on file %s! %w",
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
			return stg.Log(storage.ERROR, stg.Name,
				"Prune: Error calling stat on file %s! %w",
				candidate,
				err,
			)
		}
		if fi.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stg.Stats.Storages.Local = types.StorageStats{
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
			return stg.Log(storage.ERROR, stg.Name,
				"Prune: %d error(s) deleting local files, starting with: %w",
				len(removeErrors),
				utilites.Join(removeErrors...),
			)
		}
		return nil
	})

	return nil
}
