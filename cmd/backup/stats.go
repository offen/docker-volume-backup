// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"sync"
	"time"
)

// ContainersStats stats about the docker containers
type ContainersStats struct {
	All        uint
	ToStop     uint
	Stopped    uint
	StopErrors uint
}

// ServicesStats contains info about Swarm services that have been
// operated upon
type ServicesStats struct {
	All             uint
	ToScaleDown     uint
	ScaledDown      uint
	ScaleDownErrors uint
}

// BackupFileStats stats about the created backup file
type BackupFileStats struct {
	Name     string
	FullPath string
	Size     uint64
}

// StorageStats stats about the status of an archival directory
type StorageStats struct {
	Total       uint
	Pruned      uint
	PruneErrors uint
}

// Stats global stats regarding script execution
type Stats struct {
	sync.Mutex
	StartTime  time.Time
	EndTime    time.Time
	TookTime   time.Duration
	LockedTime time.Duration
	LogOutput  *bytes.Buffer
	Containers ContainersStats
	Services   ServicesStats
	BackupFile BackupFileStats
	Storages   map[string]StorageStats
}
