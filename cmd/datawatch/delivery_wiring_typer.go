// How a reminder or a typed request reaches a session's terminal (delivery job
// 4). Owner: part C, with the session package change that records only
// "request <id> typed". Part A passes newRequestTyper(deps) to the reminder
// sender.

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/session"
)

type requestTyper struct{ sessions *session.Manager }

// newRequestTyper returns the typer over the daemon's sessions.
func newRequestTyper(d deliveryDeps) delivery.Typer { return requestTyper{d.sessions} }

// typed turns the session manager's "nothing was typed" into the reminder
// sender's. Any other failure may have typed, and stays as it is.
func (t requestTyper) typed(do func() error) error {
	if t.sessions == nil {
		return delivery.ErrPromptRefused
	}
	err := do()
	if errors.Is(err, session.ErrNothingTyped) {
		return fmt.Errorf("%w (%v)", delivery.ErrPromptRefused, err)
	}
	return err
}

// Notice types one content-free line.
func (t requestTyper) Notice(_ context.Context, sessionID, line string) error {
	return t.typed(func() error { return t.sessions.SendRequestNotice(sessionID, line) })
}

// Request types a request's text.
func (t requestTyper) Request(_ context.Context, sessionID, requestID, text string) error {
	return t.typed(func() error { return t.sessions.SendRequestText(sessionID, requestID, text) })
}

// Ready says beforehand that nothing would be typed now.
func (t requestTyper) Ready(_ context.Context, sessionID string, content bool) error {
	return t.typed(func() error {
		if content {
			return t.sessions.CanTypeRequest(sessionID)
		}
		return t.sessions.CanTypeRequestNotice(sessionID)
	})
}
