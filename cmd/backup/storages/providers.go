package storages

import (
	"os"
	"time"

	t "github.com/offen/docker-volume-backup/cmd/backup/types"
	"github.com/sirupsen/logrus"
)

// A pool or collection of all implemented storage provider types.
type StoragePool struct {
	Local  *LocalStorage
	S3     *S3Storage
	SSH    *SSHStorage
	WebDav *WebDavStorage
}

// Init procedure for all available storage providers.
func (sp *StoragePool) InitAll(c *t.Config, l *logrus.Logger) error {
	var err error
	if sp.S3, err = InitS3(c, l); err != nil {
		return err
	}

	if sp.WebDav, err = InitWebDav(c, l); err != nil {
		return err
	}

	if sp.SSH, err = InitSSH(c, l); err != nil {
		return err
	}

	sp.Local = InitLocal(c, l)

	return nil
}

// Copy function for all available storage providers.
func (sp *StoragePool) CopyAll(file string) error {
	if sp.S3 != nil {
		if err := sp.S3.copy(file); err != nil {
			return err
		}
	}

	if sp.WebDav != nil {
		if err := sp.WebDav.copy(file); err != nil {
			return err
		}
	}

	if sp.SSH != nil {
		if err := sp.SSH.copy(file); err != nil {
			return err
		}
	}

	if _, err := os.Stat(sp.Local.config.BackupArchive); !os.IsNotExist(err) {
		if err := sp.Local.copy(file); err != nil {
			return err
		}
	}

	return nil
}

// Prune function for all available storage providers.
func (sp *StoragePool) PruneAll(deadline time.Time) error {
	if sp.S3 != nil {
		sp.S3.prune(deadline)
	}

	if sp.WebDav != nil {
		sp.WebDav.prune(deadline)
	}

	if sp.SSH != nil {
		sp.SSH.prune(deadline)
	}

	if _, err := os.Stat(sp.Local.config.BackupArchive); !os.IsNotExist(err) {
		sp.Local.prune(deadline)
	}

	return nil
}
