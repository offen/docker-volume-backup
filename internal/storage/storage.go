// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

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
	DestinationPath string
	RetentionDays   int
	Log             Log
}

type LogLevel int

const (
	LogLevelInfo LogLevel = iota
	LogLevelWarning
	LogLevelError
)

type Log func(logType LogLevel, context string, msg string, params ...any)

// PruneStats is a wrapper struct for returning stats after pruning
type PruneStats struct {
	Total  uint
	Pruned uint
}

// DoPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func (b *StorageBackend) DoPrune(context string, lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}
		b.Log(LogLevelInfo, context,
			"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
			lenMatches,
			lenCandidates,
			description,
			b.RetentionDays,
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		b.Log(LogLevelWarning, context, "The current configuration would delete all %d existing %s.", lenMatches, description)
		b.Log(LogLevelWarning, context, "Refusing to do so, please check your configuration.")
	} else {
		b.Log(LogLevelInfo, context, "None of %d existing %s were pruned.", lenCandidates, description)
	}
	return nil
}
