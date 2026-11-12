package main

import (
	"fmt"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// workerStallObservation is the input to decideWorkerStall. The caller fills
// the closures from live session state; tests pass plain funcs, so the
// decision never needs tmux or a session manager.
type workerStallObservation struct {
	LogPath   string
	WorkDir   string
	Threshold time.Duration
	Now       time.Time
	// LogLastEventAge is the age of the newest event in the backend's own
	// session log (0 = unknown). When known it replaces the terminal log's
	// mtime: a log that keeps growing is never stalled, and a log silent past
	// Threshold no longer lets a touched pane log vouch for the worker.
	LogLastEventAge time.Duration
	ChildAlive      func() bool
	BackendBusy     func() bool
}

// decideWorkerStall reports whether a worker counts as stalled, and the
// evidence-naming reason when it does. A worker is stalled only when every
// signal of life is absent for longer than Threshold: no log output, no live
// runner child, no busy backend, and no recently changed file in WorkDir.
// Threshold <= 0 never reports a stall.
func decideWorkerStall(obs workerStallObservation) (stalled bool, reason string) {
	logPath := obs.LogPath
	if obs.LogLastEventAge > 0 {
		if obs.Threshold > 0 && obs.LogLastEventAge <= obs.Threshold {
			return false, ""
		}
		logPath = ""
	}
	return workerActivityEvidence(logPath, obs.WorkDir, obs.ChildAlive, obs.BackendBusy, obs.Threshold, obs.Now)
}

// workerStallError builds the failure the verify wait loop returns for a
// stalled worker. The text names the evidence that was checked and the
// configured threshold, and wraps ErrWorkerStalled so the executor retries.
func workerStallError(reason string, threshold time.Duration) error {
	return fmt.Errorf("worker stalled: %s (stale_task_seconds=%ds): %w", reason, int(threshold.Seconds()), autonomouspkg.ErrWorkerStalled)
}
