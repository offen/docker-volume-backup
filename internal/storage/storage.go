// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package storage

import (
	"time"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
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
	Log             Log
}

type LogLevel int

const (
	LogLevelInfo LogLevel = iota
	LogLevelWarning
)

type Log func(logType LogLevel, context string, msg string, params ...any)

// PruneStats is a wrapper struct for returning stats after pruning
type PruneStats struct {
	Total  uint
	Pruned uint
}

// DoPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func (b *StorageBackend) DoPrune(context string, lenMatches, lenCandidates int, deadline time.Time, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}

		formattedDeadline, err := deadline.Local().MarshalText()
		if err != nil {
			return errwrap.Wrap(err, "error marshaling deadline")
		}
		b.Log(LogLevelInfo, context,
			"Pruned %d out of %d backups as they were older than the given deadline of %s.",
			lenMatches,
			lenCandidates,
			string(formattedDeadline),
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		b.Log(LogLevelWarning, context, "The current configuration would delete all %d existing backups.", lenMatches)
		b.Log(LogLevelWarning, context, "Refusing to do so, please check your configuration.")
	} else {
		b.Log(LogLevelInfo, context, "None of %d existing backups were pruned.", lenCandidates)
	}
	return nil
}
