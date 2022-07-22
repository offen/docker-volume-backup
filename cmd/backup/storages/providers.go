package storages

import (
	"os"
	"time"

	t "github.com/offen/docker-volume-backup/cmd/backup/types"
	"github.com/sirupsen/logrus"
)

type StorageProviders struct {
	Local  *LocalStorage
	S3     *S3Storage
	SSH    *SshStorage
	WebDav *WebDavStorage
}

func (sp *StorageProviders) InitAll(c *t.Config, l *logrus.Logger) error {
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

func (sp *StorageProviders) CopyAll(file string) error {
	if sp.S3 != nil {
		if err := sp.S3.Copy(file); err != nil {
			return err
		}
	}

	if sp.WebDav != nil {
		if err := sp.WebDav.Copy(file); err != nil {
			return err
		}
	}

	if sp.SSH != nil {
		if err := sp.SSH.Copy(file); err != nil {
			return err
		}
	}

	if _, err := os.Stat(sp.Local.config.BackupArchive); !os.IsNotExist(err) {
		if err := sp.Local.Copy(file); err != nil {
			return err
		}
	}

	return nil
}

func (sp *StorageProviders) PruneAll(deadline time.Time) error {
	if sp.S3 != nil {
		sp.S3.Prune(deadline)
	}

	if sp.WebDav != nil {
		sp.WebDav.Prune(deadline)
	}

	if sp.SSH != nil {
		sp.SSH.Prune(deadline)
	}

	if _, err := os.Stat(sp.Local.config.BackupArchive); !os.IsNotExist(err) {
		sp.Local.Prune(deadline)
	}

	return nil
}
