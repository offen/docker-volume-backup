// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"sort"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
)

// hook contains a queued action that can be trigger them when the script
// reaches a certain point (e.g. unsuccessful backup)
type hook struct {
	level  hookLevel
	action func(err error) error
}

type hookLevel int

const (
	hookLevelPlumbing hookLevel = iota
	hookLevelError
	hookLevelInfo
)

var hookLevels = map[string]hookLevel{
	"info":  hookLevelInfo,
	"error": hookLevelError,
}

// registerHook adds the given action at the given level.
func (s *script) registerHook(level hookLevel, action func(err error) error) {
	s.hooks = append(s.hooks, hook{level, action})
}

// runHooks runs all hooks that have been registered using the
// given levels in the defined ordering. In case executing a hook returns an
// error, the following hooks will still be run before the function returns.
func (s *script) runHooks(err error) error {
	sort.SliceStable(s.hooks, func(i, j int) bool {
		return s.hooks[i].level < s.hooks[j].level
	})
	var actionErrors []error
	for _, hook := range s.hooks {
		if hook.level > s.hookLevel {
			continue
		}
		if actionErr := hook.action(err); actionErr != nil {
			actionErrors = append(actionErrors, errwrap.Wrap(actionErr, "error running hook"))
		}
	}
	if len(actionErrors) != 0 {
		return errors.Join(actionErrors...)
	}
	return nil
}
