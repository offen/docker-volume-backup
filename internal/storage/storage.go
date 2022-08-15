package storage

import (
	"time"
)

// Backend is an interface for defining functions which all storage providers support.
type Backend interface {
	Copy(file string) error
	Prune(deadline time.Time, pruningPrefix string) (*PruneStats, error)
	Name() string
}

// StorageBackend is a generic type of storage. Everything here are common properties of all storage types.
type StorageBackend struct {
	Backend
	DestinationPath string
	RetentionDays   int
	Log             LogFuncDef
}

type LogType string

const (
	INFO    LogType = "INFO"
	WARNING LogType = "WARNING"
	ERROR   LogType = "ERROR"
)

type LogFuncDef func(logType LogType, context string, msg string, params ...interface{}) error

// PruneStats is a wrapper struct for returning stats after pruning
type PruneStats struct {
	Total  uint
	Pruned uint
}

// DoPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func (b *StorageBackend) DoPrune(lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}
		b.Log(INFO, b.Name(),
			"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
			lenMatches,
			lenCandidates,
			description,
			b.RetentionDays,
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		b.Log(WARNING, b.Name(), "The current configuration would delete all %d existing %s.", lenMatches, description)
		b.Log(WARNING, b.Name(), "Refusing to do so, please check your configuration.")
	} else {
		b.Log(INFO, b.Name(), "None of %d existing %s were pruned.", lenCandidates, description)
	}
	return nil
}
