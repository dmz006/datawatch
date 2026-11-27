// How a reminder or a typed request reaches a session's terminal (delivery job
// 4). Owner: part C, with the session package change that records only
// "request <id> typed". Part A passes newRequestTyper(deps) to the reminder
// sender. STUB: it types nothing.

package main

import (
	"context"

	"github.com/dmz006/datawatch/internal/delivery"
)

type requestTyper struct{}

// newRequestTyper returns the typer over the daemon's sessions.
func newRequestTyper(deliveryDeps) delivery.Typer { return requestTyper{} }

// Notice types one content-free line.
func (requestTyper) Notice(context.Context, string, string) error {
	return delivery.ErrRequestsNotBuilt
}

// Request types a request's text.
func (requestTyper) Request(context.Context, string, string, string) error {
	return delivery.ErrRequestsNotBuilt
}

var _ = newRequestTyper
