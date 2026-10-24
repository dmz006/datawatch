package main

import (
	"context"
	"log"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// attemptKiller is the slice of the session manager the stall kill needs. It
// is an interface so the pre-kill ordering can be tested with a fake.
type attemptKiller interface {
	EndSession(fullID string, why session.EndReason) error
}

// attemptSnapshotStore persists a snapshot ref on the task it was taken from.
type attemptSnapshotStore interface {
	RecordTaskAttemptSnapshot(prdID, taskID, ref string) error
}

// killStalledWorker preserves a stalled worker's work and then kills its
// session. The snapshot is taken first, while the work dir still holds the
// attempt's own state. A snapshot failure is logged and never blocks the kill.
// When preserve is false (autonomous.preserve_attempt_work off) no snapshot is
// taken and the kill proceeds as before. why is recorded on the session as its
// end.
func killStalledWorker(ctx context.Context, killer attemptKiller, store attemptSnapshotStore, preserve bool, workDir string, task *autonomouspkg.Task, sessionID string, why session.EndReason) {
	if preserve && workDir != "" {
		preserveAttemptWork(ctx, store, workDir, task)
	}
	_ = killer.EndSession(sessionID, why)
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

// attemptRestoreFunc is autonomouspkg.RestoreAttemptWork as a value, so the
// spawn-time ordering can be tested with a stub.
type attemptRestoreFunc func(ctx context.Context, dir, ref string) error

// restoreAttemptWorkBeforeSpawn is the attempt-restore step of the spawn
// closure. capture always runs first: it records the pre-task SHA, untracked
// files and dirty files, so the restored files are never reported as foreign
// drift. Only after that does restore write the last recorded attempt ref back
// into dir. A restore error is logged and never blocks the attempt, which then
// starts from the pre-task state. cleanRestart (an operator clean restart,
// Task.CleanRestart) skips the restore so the attempt starts from the pre-task
// state on purpose.
func restoreAttemptWorkBeforeSpawn(ctx context.Context, dir string, refs []string, enabled, cleanRestart bool, capture func(), restore attemptRestoreFunc) {
	capture()
	if !enabled || dir == "" || len(refs) == 0 {
		return
	}
	ref := refs[len(refs)-1]
	if cleanRestart {
		log.Printf("[autonomous] skipped restore of attempt work from %s into %s: clean restart requested", ref, dir)
		return
	}
	if err := restore(ctx, dir, ref); err != nil {
		log.Printf("[autonomous] restore of attempt work from %s into %s failed, attempt starts from pre-task state: %v",
			ref, dir, err)
		return
	}
	log.Printf("[autonomous] restored attempt work from %s into %s before spawn", ref, dir)
}
