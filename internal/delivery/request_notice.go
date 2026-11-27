// Reminders: one content-free line per session saying how many requests wait.
// (notify.go is the other direction, the messages to the operator.) Owner: part
// C. STUB.

package delivery

import (
	"context"
	"time"
)

// NotifierOptions configure the reminders.
type NotifierOptions struct {
	Typer    Typer
	Holds    Holds
	Sessions SessionBook
	Roles    RoleKeeper
	// Verifier is told of a request's first reminder or typed input.
	Verifier Verifier
	Settings func() RequestSettings
}

type notifier struct{ opts NotifierOptions }

// NewNotifier returns the reminder sender.
func NewNotifier(opts NotifierOptions) Notifier { return &notifier{opts: opts} }

func (n *notifier) Notify(context.Context, RequestBook, time.Time) error { return ErrRequestsNotBuilt }
