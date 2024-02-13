// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import "runtime"

func (c *command) profile() {
	memStats := runtime.MemStats{}
	runtime.ReadMemStats(&memStats)
	c.logger.Info(
		"Collecting runtime information",
		"num_goroutines",
		runtime.NumGoroutine(),
		"memory_heap_alloc",
		formatBytes(memStats.HeapAlloc, false),
		"memory_heap_inuse",
		formatBytes(memStats.HeapInuse, false),
		"memory_heap_sys",
		formatBytes(memStats.HeapSys, false),
		"memory_heap_objects",
		memStats.HeapObjects,
	)
}
