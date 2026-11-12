package main

import (
	"errors"
	"fmt"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// mapSessionStartError maps a session-package error that means "the worker
// session never actually started" onto the autonomous sentinel
// ErrSessionNeverStarted, which is the only start-failure signal the
// autonomous executor understands (internal/autonomous must not import
// internal/session — that would create an import cycle — so the mapping
// lives at this cmd/datawatch boundary, where the wait loop or prompt
// delivery error is turned into a task error).
//
// An error carrying session.ErrPromptNotSubmitted gains the autonomous
// sentinel while keeping its cause; errors that do not carry it (including
// nil and already-mapped errors) pass through unchanged.
func mapSessionStartError(err error) error {
	if err == nil || !errors.Is(err, session.ErrPromptNotSubmitted) {
		return err
	}
	if errors.Is(err, autonomouspkg.ErrSessionNeverStarted) {
		return err // already carries the autonomous sentinel
	}
	return fmt.Errorf("%w: %w", autonomouspkg.ErrSessionNeverStarted, err)
}
