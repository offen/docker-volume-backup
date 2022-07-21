package main

import "time"

type Helper interface {
	copyArchive(name string) error
	pruneBackups(deadline time.Time) error
}

type AbstractHelper struct {
	Helper
}

// doPrune holds general control flow that applies to any kind of storage.
// Callers can pass in a thunk that performs the actual deletion of files.
func doPrune(s *script, lenMatches, lenCandidates int, description string, doRemoveFiles func() error) error {
	if lenMatches != 0 && lenMatches != lenCandidates {
		if err := doRemoveFiles(); err != nil {
			return err
		}
		s.logger.Infof(
			"Pruned %d out of %d %s as their age exceeded the configured retention period of %d days.",
			lenMatches,
			lenCandidates,
			description,
			s.c.BackupRetentionDays,
		)
	} else if lenMatches != 0 && lenMatches == lenCandidates {
		s.logger.Warnf("The current configuration would delete all %d existing %s.", lenMatches, description)
		s.logger.Warn("Refusing to do so, please check your configuration.")
	} else {
		s.logger.Infof("None of %d existing %s were pruned.", lenCandidates, description)
	}
	return nil
}
