// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package local

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/offen/docker-volume-backup/internal/errwrap"
	"github.com/offen/docker-volume-backup/internal/storage"
)

type localStorage struct {
	*storage.StorageBackend
	latestSymlink string
}

// Config allows configuration of a local storage backend.
type Config struct {
	ArchivePath   string
	LatestSymlink string
}

// NewStorageBackend creates and initializes a new local storage backend.
func NewStorageBackend(opts Config, logFunc storage.Log) storage.Backend {
	return &localStorage{
		StorageBackend: &storage.StorageBackend{
			DestinationPath: opts.ArchivePath,
			Log:             logFunc,
		},
		latestSymlink: opts.LatestSymlink,
	}
}

// Name return the name of the storage backend
func (b *localStorage) Name() string {
	return "Local"
}

// Copy copies the given file to the local storage backend.
func (b *localStorage) Copy(file string) error {
	_, name := path.Split(file)

	if err := copyFile(file, path.Join(b.DestinationPath, name)); err != nil {
		return errwrap.Wrap(err, "error copying file to archive")
	}
	b.Log(storage.LogLevelInfo, b.Name(), "Stored copy of backup `%s` in `%s`.", file, b.DestinationPath)

	if b.latestSymlink != "" {
		symlink := path.Join(b.DestinationPath, b.latestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			if err := os.Remove(symlink); err != nil {
				return errwrap.Wrap(err, "error removing existing symlink")
			}
		}
		if err := os.Symlink(name, symlink); err != nil {
			return errwrap.Wrap(err, "error creating latest symlink")
		}
		b.Log(storage.LogLevelInfo, b.Name(), "Created/Updated symlink `%s` for latest backup.", b.latestSymlink)
	}

	return nil
}

// Prune rotates away backups according to the configuration and provided deadline for the local storage backend.
func (b *localStorage) Prune(deadline time.Time, pruningPrefix string) (*storage.PruneStats, error) {
	globPattern := path.Join(
		b.DestinationPath,
		fmt.Sprintf("%s*", pruningPrefix),
	)
	globMatches, err := filepath.Glob(globPattern)
	if err != nil {
		return nil, errwrap.Wrap(
			err,
			fmt.Sprintf(
				"error looking up matching files using pattern %s",
				globPattern,
			),
		)
	}

	var candidates []string
	for _, candidate := range globMatches {
		fi, err := os.Lstat(candidate)
		if err != nil {
			return nil, errwrap.Wrap(
				err,
				fmt.Sprintf(
					"error calling Lstat on file %s",
					candidate,
				),
			)
		}

		if !fi.IsDir() && fi.Mode()&os.ModeSymlink != os.ModeSymlink {
			candidates = append(candidates, candidate)
		}
	}

	var matches []string
	for _, candidate := range candidates {
		fi, err := os.Stat(candidate)
		if err != nil {
			return nil, errwrap.Wrap(
				err,
				fmt.Sprintf(
					"error calling stat on file %s",
					candidate,
				),
			)
		}
		if fi.ModTime().Before(deadline) {
			matches = append(matches, candidate)
		}
	}

	stats := &storage.PruneStats{
		Total:  uint(len(candidates)),
		Pruned: uint(len(matches)),
	}

	pruneErr := b.DoPrune(b.Name(), len(matches), len(candidates), deadline, func() error {
		var removeErrors []error
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				removeErrors = append(removeErrors, err)
			}
		}
		if len(removeErrors) != 0 {
			return errwrap.Wrap(
				errors.Join(removeErrors...),
				fmt.Sprintf(
					"%d error(s) deleting files",
					len(removeErrors),
				),
			)
		}
		return nil
	})

	return stats, pruneErr
}

// copy creates a copy of the file located at `dst` at `src`.
func copyFile(src, dst string) (returnErr error) {
	in, err := os.Open(src)
	if err != nil {
		returnErr = err
		return
	}
	defer func() {
		returnErr = in.Close()
	}()

	out, err := os.Create(dst)
	if err != nil {
		returnErr = err
		return
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return errors.Join(err, out.Close())
	}
	return out.Close()
}
