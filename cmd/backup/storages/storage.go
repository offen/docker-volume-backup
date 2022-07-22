package storages

import (
	"time"

	t "github.com/offen/docker-volume-backup/cmd/backup/types"
	"github.com/sirupsen/logrus"
)

// Interface for defining functions which all storage providers support.
type Storage interface {
	copy(file string) error
	prune(deadline time.Time) (*t.StorageStats, error)
}

// Generic type of storage. Everything here are common properties of all storage types.
type GenericStorage struct {
	Storage
	logger *logrus.Logger
	config *t.Config
}

// doPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func (stg *GenericStorage) doPrune(lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}
		stg.logger.Infof(
			"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
			lenMatches,
			lenCandidates,
			description,
			stg.config.BackupRetentionDays,
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		stg.logger.Warnf("The current configuration would delete all %d existing %s.", lenMatches, description)
		stg.logger.Warn("Refusing to do so, please check your configuration.")
	} else {
		stg.logger.Infof("None of %d existing %s were pruned.", lenCandidates, description)
	}
	return nil
}
