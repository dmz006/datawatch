package main

import (
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// exitSilenceTick advances the exit-silence tracker for one wait-loop tick
// and reports whether the run has been silent past the configured limit with
// no live runner. It only applies to the pi-prompt family: one-shot
// `pi --print` runs write nothing to the log until they finish, so a silent,
// exited runner is the failure mode to catch here (see staleWorkerAppliesTo,
// which deliberately excludes pi-prompt from the log-mtime stall check).
// For any other family — or a nil tracker — it returns (false, "") without
// calling either probe, leaving the existing detectors in charge.
func exitSilenceTick(tr *autonomouspkg.ExitSilenceTracker, backendFamily string, now time.Time, runnerAlive func() bool, logModTime func() time.Time) (stalled bool, reason string) {
	if tr == nil || backendFamily != "pi-prompt" {
		return false, ""
	}
	return tr.Observe(now, runnerAlive(), logModTime())
}
