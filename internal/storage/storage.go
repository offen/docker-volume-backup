package storage

import (
	"time"

	t "github.com/offen/docker-volume-backup/internal/types"
	"github.com/sirupsen/logrus"
)

// Interface for defining functions which all storage providers support.
type Backend interface {
	Copy(file string) error
	Prune(deadline time.Time, pruningPrefix string) error
}

// Generic type of storage. Everything here are common properties of all storage types.
type StorageBackend struct {
	Backend
	Name            string
	DestinationPath string
	RetentionDays   int
	Logger          *logrus.Logger
	Stats           *t.Stats
}

// DoPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func (stg *StorageBackend) DoPrune(lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}
		stg.Logger.Infof(
			"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
			lenMatches,
			lenCandidates,
			description,
			stg.RetentionDays,
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		stg.Logger.Warnf("The current configuration would delete all %d existing %s.", lenMatches, description)
		stg.Logger.Warn("Refusing to do so, please check your configuration.")
	} else {
		stg.Logger.Infof("None of %d existing %s were pruned.", lenCandidates, description)
	}
	return nil
}
