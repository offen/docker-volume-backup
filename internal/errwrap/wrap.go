// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package errwrap

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// Wrap wraps the given error using the given message while prepending
// the name of the calling function, creating a poor man's stack trace
func Wrap(err error, msg string) error {
	pc := make([]uintptr, 15)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	frame, _ := frames.Next()
	// strip full import paths and just use the package name
	chunks := strings.Split(frame.Function, "/")
	withCaller := fmt.Sprintf("%s: %s", chunks[len(chunks)-1], msg)
	if err == nil {
		return fmt.Errorf(withCaller)
	}
	return fmt.Errorf("%s: %w", withCaller, err)
}

// Unwrap receives an error and returns the last error in the chain of
// wrapped errors
func Unwrap(err error) error {
	if err == nil {
		return nil
	}
	for {
		u := errors.Unwrap(err)
		if u == nil {
			break
		}
		err = u
	}
	return err
}
