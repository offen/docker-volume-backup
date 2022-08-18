package local

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/offen/docker-volume-backup/internal/storage"
	"github.com/offen/docker-volume-backup/internal/utilities"
)

type localStorage struct {
	*storage.StorageBackend
	latestSymlink string
}

// Options allows configuration of a local storage backend.
type Options struct {
	ArchivePath   string
	LatestSymlink string
}

// NewStorageBackend creates and initializes a new local storage backend.
func NewStorageBackend(opts Options, logFunc storage.Log) storage.Backend {
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
		return fmt.Errorf("(*localStorage).Copy: Error copying file to local archive! %w", err)
	}
	b.Log(storage.INFO, b.Name(), "Stored copy of backup `%s` in local archive `%s`.", file, b.DestinationPath)

	if b.latestSymlink != "" {
		symlink := path.Join(b.DestinationPath, b.latestSymlink)
		if _, err := os.Lstat(symlink); err == nil {
			os.Remove(symlink)
		}
		if err := os.Symlink(name, symlink); err != nil {
			return fmt.Errorf("(*localStorage).Copy: error creating latest symlink! %w", err)
		}
		b.Log(storage.INFO, b.Name(), "Created/Updated symlink `%s` for latest backup.", b.latestSymlink)
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
		return nil, fmt.Errorf(
			"(*localStorage).Prune: Error looking up matching files using pattern %s: %w",
			globPattern,
			err,
		)
	}

	var candidates []string
	for _, candidate := range globMatches {
		fi, err := os.Lstat(candidate)
		if err != nil {
			return nil, fmt.Errorf(
				"(*localStorage).Prune: Error calling Lstat on file %s: %w",
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
				"(*localStorage).Prune: Error calling stat on file %s! %w",
				candidate,
				err,
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

	if err := b.DoPrune(b.Name(), len(matches), len(candidates), "local backup(s)", func() error {
		var removeErrors []error
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				removeErrors = append(removeErrors, err)
			}
		}
		if len(removeErrors) != 0 {
			return fmt.Errorf(
				"(*localStorage).Prune: %d error(s) deleting local files, starting with: %w",
				len(removeErrors),
				utilities.Join(removeErrors...),
			)
		}
		return nil
	}); err != nil {
		return stats, err
	}

	return stats, nil
}

// copy creates a copy of the file located at `dst` at `src`.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
