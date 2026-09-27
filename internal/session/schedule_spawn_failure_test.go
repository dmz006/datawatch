package session

import (
	"testing"
	"time"
)

func newSpawnSched(t *testing.T, opts AddSpawnOptions) (*ScheduleStore, *ScheduledCommand) {
	t.Helper()
	store, err := NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := store.AddSpawn(opts)
	if err != nil {
		t.Fatal(err)
	}
	return store, sc
}

// Regression: a single failed start of a recurring spawn (session cap, tmux
// error, ...) used to mark the schedule failed forever, silently ending an
// hourly job.
func TestSpawnFailure_CronScheduleIsRearmedNotFailed(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", CronExpr: "0 * * * *", Subprocess: true, OneShot: true})
	before := sc.RunAt
	n, rearmed, err := store.RecordSpawnFailure(sc.ID)
	if err != nil || n != 1 || !rearmed {
		t.Fatalf("n=%d rearmed=%v err=%v", n, rearmed, err)
	}
	got, _ := store.Get(sc.ID)
	if got.State != SchedPending || got.LastFireResult != "failed" || got.FireCount != 1 {
		t.Fatalf("recurring schedule must stay pending after a failed fire: %+v", got)
	}
	if !got.RunAt.After(time.Now()) || got.RunAt.Equal(before) && before.Before(time.Now()) {
		t.Fatalf("next fire must be in the future, got %v", got.RunAt)
	}
}

func TestSpawnFailure_ConsecutiveCountsAndSuccessResets(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", CronExpr: "0 * * * *"})
	store.RecordSpawnFailure(sc.ID) //nolint:errcheck
	n, _, _ := store.RecordSpawnFailure(sc.ID)
	if n != 2 {
		t.Fatalf("want 2 consecutive failures, got %d", n)
	}
	_ = store.RecordFire(sc.ID, "sess-1", "spawned")
	got, _ := store.Get(sc.ID)
	if got.ConsecutiveFailures != 0 {
		t.Fatalf("a successful spawn must reset the counter, got %d", got.ConsecutiveFailures)
	}
}

func TestSpawnFailure_OneTimeScheduleBecomesFailed(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", RunAt: time.Now().Add(time.Minute)})
	_, rearmed, _ := store.RecordSpawnFailure(sc.ID)
	got, _ := store.Get(sc.ID)
	if rearmed || got.State != SchedFailed {
		t.Fatalf("one-time spawn should end failed: rearmed=%v state=%s", rearmed, got.State)
	}
}

func TestSpawnFailure_CancelledScheduleStaysCancelled(t *testing.T) {
	store, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", CronExpr: "0 * * * *"})
	_ = store.Cancel(sc.ID)
	store.RecordSpawnFailure(sc.ID) //nolint:errcheck
	got, _ := store.Get(sc.ID)
	if got.State != SchedCancelled {
		t.Fatalf("cancelled must stay cancelled, got %s", got.State)
	}
}
