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

// idleLivenessProbe returns the liveness probe that feeds the idle observation
// for a backend family. A one-shot pi print run generates with no child
// process, so for the pi-prompt family the question is whether the runner is
// alive; for every other family it stays whether the runner has a work child.
func idleLivenessProbe(backendFamily string) func(tmuxSession, projectDir string) bool {
	return selectIdleLivenessProbe(backendFamily,
		func(tmuxSession, _ string) bool { return hasLiveRunnerProcess(tmuxSession) },
		hasLiveChildProcess)
}

// selectIdleLivenessProbe is the pure choice behind idleLivenessProbe, with the
// two probes injected.
func selectIdleLivenessProbe(backendFamily string, runnerProbe, childProbe func(tmuxSession, projectDir string) bool) func(tmuxSession, projectDir string) bool {
	if backendFamily == "pi-prompt" {
		return runnerProbe
	}
	return childProbe
}
