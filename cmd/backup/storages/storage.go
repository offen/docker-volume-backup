package storages

import (
	"time"

	t "github.com/offen/docker-volume-backup/cmd/backup/types"
	"github.com/sirupsen/logrus"
)

type Storage interface {
	Copy(file string) error
	Prune(deadline time.Time) (*t.StorageStats, error)
}

type GenericStorage struct {
	Storage
	backupRetentionDays int32
	backupPruningPrefix string
	logger              *logrus.Logger
}

// doPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func (s *GenericStorage) doPrune(lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}
		s.logger.Infof(
			"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
			lenMatches,
			lenCandidates,
			description,
			s.backupRetentionDays,
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		s.logger.Warnf("The current configuration would delete all %d existing %s.", lenMatches, description)
		s.logger.Warn("Refusing to do so, please check your configuration.")
	} else {
		s.logger.Infof("None of %d existing %s were pruned.", lenCandidates, description)
	}
	return nil
}
