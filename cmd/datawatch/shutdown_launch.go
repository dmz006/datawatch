package main

import (
	"fmt"
	"io"
	"time"
)

// shutdownLaunchGrace is how long a daemon shutdown waits for session launches
// still in flight before it rolls them back. A healthy backend launch confirms
// within a few seconds, so 15 s lets almost every launch finish cleanly. It is
// a fixed value, not a config key. The rollback's own settle wait (about 2 s)
// and the HTTP shutdown wait (shutdownHTTPWait) come on top, so the whole stop
// stays well inside the 90 s default systemd stop timeout and the service
// manager never escalates to SIGKILL while the drain runs.
const shutdownLaunchGrace = 15 * time.Second

// shutdownHTTPWait bounds how long the shutdown path waits for the HTTP server
// to return after its context is cancelled. It sits just above the 5 s
// Shutdown timeout inside internal/server, so the wait ends when the server
// has actually stopped accepting work.
const shutdownHTTPWait = 6 * time.Second

// launchDrainer is the slice of the session manager the shutdown path needs.
type launchDrainer interface {
	DrainLaunches(grace time.Duration) []string
}

// drainLaunchesOnShutdown refuses new launches and rolls back the ones still
// running after shutdownLaunchGrace. It writes one line per rolled-back session
// to out and nothing when every launch finished inside the grace.
func drainLaunchesOnShutdown(d launchDrainer, out io.Writer) {
	for _, id := range d.DrainLaunches(shutdownLaunchGrace) {
		fmt.Fprintf(out, "[session] rolled back launch %s: still running at shutdown\n", id)
	}
}
