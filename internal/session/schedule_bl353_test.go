package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore353(t *testing.T) *ScheduleStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewScheduleStore(filepath.Join(dir, "sched.json"))
	if err != nil {
		t.Fatalf("NewScheduleStore: %v", err)
	}
	return store
}

func TestBL353_AddFull_SessionName(t *testing.T) {
	store := newTestStore353(t)

	sc, err := store.AddFull(AddOptions{
		SessionName: "my-session",
		Command:     "continue",
		RunAt:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("AddFull: %v", err)
	}
	if sc.SessionName != "my-session" {
		t.Errorf("SessionName: got %q, want %q", sc.SessionName, "my-session")
	}
	if sc.SessionID != "" {
		t.Errorf("SessionID should be empty, got %q", sc.SessionID)
	}
	if sc.State != SchedPending {
		t.Errorf("State: got %q, want %q", sc.State, SchedPending)
	}

	// Verify persistence
	got, ok := store.Get(sc.ID)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.SessionName != "my-session" {
		t.Errorf("persisted SessionName: got %q, want %q", got.SessionName, "my-session")
	}
}

func TestBL353_AddFull_CronExpr(t *testing.T) {
	store := newTestStore353(t)

	sc, err := store.AddFull(AddOptions{
		SessionID: "sess-abc",
		Command:   "run-task",
		CronExpr:  "*/5 * * * *",
		RunAt:     time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("AddFull: %v", err)
	}
	if sc.CronExpr != "*/5 * * * *" {
		t.Errorf("CronExpr: got %q, want %q", sc.CronExpr, "*/5 * * * *")
	}

	// Simulate MarkDone — should reschedule via cron
	beforeMarkDone := time.Now()
	if err := store.MarkDone(sc.ID, false); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	updated, ok := store.Get(sc.ID)
	if !ok {
		t.Fatal("Get after MarkDone: not found")
	}
	if updated.State != SchedPending {
		t.Errorf("after MarkDone with cron, State: got %q, want pending", updated.State)
	}
	// Next cron fire should be after the time MarkDone was called
	if !updated.RunAt.After(beforeMarkDone) {
		t.Errorf("after cron reschedule, RunAt %v should be after MarkDone time %v", updated.RunAt, beforeMarkDone)
	}
}

func TestBL353_CancelByScheduleName(t *testing.T) {
	store := newTestStore353(t)

	sc1, _ := store.AddFull(AddOptions{SessionID: "s1", Command: "cmd1", ScheduleName: "my-job", RunAt: time.Now().Add(time.Hour)})
	sc2, _ := store.AddFull(AddOptions{SessionID: "s2", Command: "cmd2", ScheduleName: "my-job", RunAt: time.Now().Add(time.Hour)})
	_, _ = store.AddFull(AddOptions{SessionID: "s3", Command: "cmd3", ScheduleName: "other-job", RunAt: time.Now().Add(time.Hour)})

	n := store.CancelByScheduleName("my-job")
	if n != 2 {
		t.Errorf("CancelByScheduleName: cancelled %d, want 2", n)
	}

	// Verify states
	got1, _ := store.Get(sc1.ID)
	got2, _ := store.Get(sc2.ID)
	if got1.State != SchedCancelled {
		t.Errorf("sc1 state: got %q, want cancelled", got1.State)
	}
	if got2.State != SchedCancelled {
		t.Errorf("sc2 state: got %q, want cancelled", got2.State)
	}

	// "other-job" should still be pending
	pending := store.List(SchedPending)
	if len(pending) != 1 {
		t.Errorf("pending after cancel: got %d, want 1", len(pending))
	}
}

func TestBL353_GetByScheduleName(t *testing.T) {
	store := newTestStore353(t)

	_, _ = store.AddFull(AddOptions{SessionID: "s1", Command: "cmd1", ScheduleName: "nightly", RunAt: time.Now().Add(time.Hour)})

	got, ok := store.GetByScheduleName("nightly")
	if !ok {
		t.Fatal("GetByScheduleName: not found")
	}
	if got.ScheduleName != "nightly" {
		t.Errorf("ScheduleName: got %q, want nightly", got.ScheduleName)
	}

	// After cancel, should not be found
	_ = store.CancelByScheduleName("nightly")
	_, ok = store.GetByScheduleName("nightly")
	if ok {
		t.Error("GetByScheduleName should return false after cancel")
	}
}

func TestBL353_CronExpr_MarkDone_Failed(t *testing.T) {
	store := newTestStore353(t)

	sc, _ := store.AddFull(AddOptions{
		SessionID: "sess",
		Command:   "cmd",
		CronExpr:  "*/10 * * * *",
		RunAt:     time.Now().Add(10 * time.Minute),
	})

	// MarkDone with failed=true should NOT reschedule via cron
	if err := store.MarkDone(sc.ID, true); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	got, _ := store.Get(sc.ID)
	if got.State != SchedFailed {
		t.Errorf("after failed MarkDone, State: got %q, want failed", got.State)
	}
}

func TestBL353_AddFull_ScheduleName(t *testing.T) {
	store := newTestStore353(t)

	sc, err := store.AddFull(AddOptions{
		SessionID:    "sess-xyz",
		Command:      "deploy",
		ScheduleName: "weekly-deploy",
		RunAt:        time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("AddFull: %v", err)
	}
	if sc.ScheduleName != "weekly-deploy" {
		t.Errorf("ScheduleName: got %q, want weekly-deploy", sc.ScheduleName)
	}

	// Cleanup
	os.RemoveAll(filepath.Dir(store.path))
}
