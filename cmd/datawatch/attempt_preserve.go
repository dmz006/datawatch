package main

import (
	"context"
	"log"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// attemptKiller is the slice of the session manager the stall kill needs. It
// is an interface so the pre-kill ordering can be tested with a fake.
type attemptKiller interface {
	Kill(fullID string) error
}

// attemptSnapshotStore persists a snapshot ref on the task it was taken from.
type attemptSnapshotStore interface {
	RecordTaskAttemptSnapshot(prdID, taskID, ref string) error
}

// killStalledWorker preserves a stalled worker's work and then kills its
// session. The snapshot is taken first, while the work dir still holds the
// attempt's own state. A snapshot failure is logged and never blocks the kill.
// When preserve is false (autonomous.preserve_attempt_work off) no snapshot is
// taken and the kill proceeds as before.
func killStalledWorker(ctx context.Context, killer attemptKiller, store attemptSnapshotStore, preserve bool, workDir string, task *autonomouspkg.Task, sessionID string) {
	if preserve && workDir != "" {
		preserveAttemptWork(ctx, store, workDir, task)
	}
	_ = killer.Kill(sessionID)
}

// preserveAttemptWork snapshots workDir under the task's next attempt number
// and records the ref on the task. Every failure is logged, none is returned.
func preserveAttemptWork(ctx context.Context, store attemptSnapshotStore, workDir string, task *autonomouspkg.Task) {
	attempt := len(task.AttemptRefs) + 1
	ref, err := autonomouspkg.SnapshotAttemptWork(ctx, workDir, task.ID, attempt)
	if err != nil {
		log.Printf("[autonomous] task %s/%s: snapshot of attempt %d failed, attempt work could not be preserved: %v",
			task.PRDID, task.ID, attempt, err)
		return
	}
	if ref == "" {
		return
	}
	log.Printf("[autonomous] task %s/%s: preserved attempt %d work at %s before kill",
		task.PRDID, task.ID, attempt, ref)
	if store == nil {
		return
	}
	if err := store.RecordTaskAttemptSnapshot(task.PRDID, task.ID, ref); err != nil {
		log.Printf("[autonomous] task %s/%s: RecordTaskAttemptSnapshot(%s): %v", task.PRDID, task.ID, ref, err)
	}
}
