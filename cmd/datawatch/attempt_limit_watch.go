package main

import (
	"context"
	"log"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// attemptLimitWatcher ties together the attempt progress clock, the stall
// kill and the attempt-limit error type. Each tick it observes the attempt's
// progress signals, lets the clock decide whether a limit (maximum attempt
// time or no-progress time) has been exceeded, and when a stop is due it
// preserves the attempt work under refs/datawatch/attempts/ (always, even if
// autonomous.preserve_attempt_work is off — this stop must never discard work)
// before killing the worker session, then returns an
// autonomouspkg.AttemptLimitError so the executor's stall retry path handles
// it exactly like a normal stall.
type attemptLimitWatcher struct {
	clock     *attemptProgressClock
	killer    attemptKiller
	store     attemptSnapshotStore
	workDir   string
	task      *autonomouspkg.Task
	sessionID string
}

// newAttemptLimitWatcher seeds the clock with the attempt start time and the
// first observation.
func newAttemptLimitWatcher(start time.Time, first attemptObservation, killer attemptKiller, store attemptSnapshotStore, workDir string, task *autonomouspkg.Task, sessionID string) *attemptLimitWatcher {
	return &attemptLimitWatcher{
		clock:     newAttemptProgressClock(start, first),
		killer:    killer,
		store:     store,
		workDir:   workDir,
		task:      task,
		sessionID: sessionID,
	}
}

// check observes the attempt at obs.Now and applies the two limits. With
// both limits <= 0 nothing is ever stopped and no kill or snapshot happens.
// When a stop is due the work is always preserved first (a snapshot failure
// is logged and never blocks the kill) and the returned error wraps
// autonomouspkg.ErrWorkerStalled.
func (w *attemptLimitWatcher) check(obs attemptObservation, maxAttempt, noProgress time.Duration) error {
	w.clock.observe(obs)
	if maxAttempt <= 0 && noProgress <= 0 {
		return nil
	}

	stop, reason := w.clock.decide(obs, maxAttempt, noProgress)
	if !stop {
		return nil
	}

	log.Printf("[autonomous] task %s/%s: stopping attempt: %s", w.task.PRDID, w.task.ID, reason)
	// preserve=true always: a task-level attempt-limit stop must never
	// discard work, regardless of autonomous.preserve_attempt_work.
	killStalledWorker(context.Background(), w.killer, w.store, true, w.workDir, w.task, w.sessionID)
	return autonomouspkg.AttemptLimitError(reason, int(maxAttempt.Seconds()), int(noProgress.Seconds()))
}
