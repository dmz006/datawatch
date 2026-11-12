// Comm-channel restart guard. A `restart` message refuses, with the read-only
// interruption report, when running Automata work would be interrupted, unless
// the message carries the word force. See docs/howto/restart-with-work-running.md.

package router

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// restartMessageHasForce reports whether a lower-cased restart message carries
// the word force after the verb, as in "restart force" or "restart now force".
func restartMessageHasForce(lower string) bool {
	fields := strings.Fields(lower)
	for _, f := range fields[min(1, len(fields)):] {
		if f == "force" {
			return true
		}
	}
	return false
}

// restartInterruptionReport reads GET /api/autonomous/restart-interruptions
// through the loopback. ok is false when the daemon's report cannot be read.
func (r *Router) restartInterruptionReport() (autonomous.RestartInterruption, bool) {
	body, err := r.commGet("/api/autonomous/restart-interruptions", url.Values{})
	if err != nil {
		return autonomous.RestartInterruption{}, false
	}
	rep, err := autonomous.ParseRestartInterruption([]byte(body))
	if err != nil {
		return autonomous.RestartInterruption{}, false
	}
	return rep, true
}

// handleRestart answers the comm `restart` verb. It refuses with the report
// when a restart would interrupt running work and the message has no force.
func (r *Router) handleRestart(cmd Command) {
	if r.restartFn == nil && r.restartForceFn == nil {
		r.send(fmt.Sprintf("[%s] restart not available.", r.hostname))
		return
	}
	if !cmd.RestartForce {
		if rep, ok := r.restartInterruptionReport(); ok && rep.NeedsForce {
			r.send(fmt.Sprintf("[%s] restart refused: running work would be interrupted.\n%s"+
				"To restart anyway, send: restart force", r.hostname, autonomous.RestartInterruptionText(rep)))
			return
		}
	}
	r.send(fmt.Sprintf("[%s] Restarting daemon…", r.hostname))
	force := cmd.RestartForce
	go func() {
		time.Sleep(500 * time.Millisecond)
		if r.restartForceFn != nil {
			r.restartForceFn(force)
			return
		}
		r.restartFn()
	}()
}
