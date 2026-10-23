package autonomous

import (
	"fmt"
	"time"
)

// VerifierWatchdog decides when a verifier session has made no progress. A
// session counts as progressing when its output file exists or its pane
// output marker changed since the previous observation; any such activity
// resets the timer. The clock is injected through the observation so the
// decision is deterministic in tests.
type VerifierWatchdog struct {
	limit        time.Duration
	lastActivity time.Time
	lastMarker   string
	seenMarker   bool
}

// NewVerifierWatchdog returns a watchdog whose silence is measured from
// start. limit <= 0 disables it (never exceeded).
func NewVerifierWatchdog(start time.Time, limit time.Duration) *VerifierWatchdog {
	return &VerifierWatchdog{limit: limit, lastActivity: start}
}

// Observe records one poll and reports whether the no-progress limit is
// exceeded. outputPresent is true when the verifier output file exists; marker
// is any stable fingerprint of the captured pane output (a change counts as
// activity, the first marker seen is the baseline and does not).
func (w *VerifierWatchdog) Observe(now time.Time, outputPresent bool, marker string) bool {
	if outputPresent {
		w.lastActivity = now
	}
	if w.seenMarker && marker != w.lastMarker {
		w.lastActivity = now
	}
	w.lastMarker, w.seenMarker = marker, true
	return w.limit > 0 && now.Sub(w.lastActivity) >= w.limit
}

// VerifierWatchdogMessage is the OneShotNoOutputError text for a tripped
// watchdog.
func VerifierWatchdogMessage(limit time.Duration) string {
	return fmt.Sprintf("verifier no-progress watchdog: no output file or pane activity for %v", limit)
}
