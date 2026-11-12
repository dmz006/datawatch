package main

import (
	"context"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// finaliserInterval turns a minutes source into the interval function the
// session finaliser takes. The source is called every time the interval is
// read, so a changed setting shows up in the next wait without a restart.
func finaliserInterval(minutes func() int) func() time.Duration {
	return func() time.Duration {
		return time.Duration(minutes()) * time.Minute
	}
}

// startSessionFinaliser starts the background finaliser on the session
// manager. It runs once straight away and then on a schedule that follows the
// value minutes returns on each cycle. The daemon passes a function that reads
// the live finalise interval setting; zero switches scheduled runs off. The
// finaliser stops when ctx is cancelled.
func startSessionFinaliser(ctx context.Context, mgr *session.Manager, minutes func() int) {
	mgr.StartFinaliser(ctx, finaliserInterval(minutes))
}
