package storage

import (
	"time"

	t "github.com/offen/docker-volume-backup/internal/types"
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
	Log             func(logType LogType, msg string, params ...interface{})
	Stats           *t.Stats
}

type LogType string

const (
	INFO    LogType = "INFO"
	WARNING LogType = "WARNING"
	ERROR   LogType = "ERROR"
)

// DoPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func (stg *StorageBackend) DoPrune(lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}
		stg.Log(INFO,
			"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
			lenMatches,
			lenCandidates,
			description,
			stg.RetentionDays,
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		stg.Log(WARNING, "The current configuration would delete all %d existing %s.", lenMatches, description)
		stg.Log(WARNING, "Refusing to do so, please check your configuration.")
	} else {
		stg.Log(INFO, "None of %d existing %s were pruned.", lenCandidates, description)
	}
	return nil
}
