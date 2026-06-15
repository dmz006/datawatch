package session

import (
	"os"
	"testing"
	"time"
)

func TestCancelBySession(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) //nolint:errcheck // Remove so NewScheduleStore creates fresh
	defer os.Remove(tmp.Name()) //nolint:errcheck

	store, err := NewScheduleStore(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Add commands for two sessions
	store.Add("host-abc1", "cmd1", time.Time{}, "") //nolint:errcheck
	store.Add("host-abc1", "cmd2", time.Now().Add(time.Hour), "") //nolint:errcheck
	store.Add("host-def2", "cmd3", time.Time{}, "") //nolint:errcheck

	// Cancel by full ID
	n := store.CancelBySession("host-abc1")
	if n != 2 {
		t.Errorf("expected 2 cancelled, got %d", n)
	}

	// Verify abc1 commands are cancelled
	pending := store.PendingForSession("host-abc1")
	if len(pending) != 0 {
		t.Errorf("expected 0 pending for abc1, got %d", len(pending))
	}

	// Verify def2 is untouched
	pending2 := store.PendingForSession("host-def2")
	if len(pending2) != 1 {
		t.Errorf("expected 1 pending for def2, got %d", len(pending2))
	}
}

func TestCancelBySessionShortID(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) //nolint:errcheck
	defer os.Remove(tmp.Name()) //nolint:errcheck

	store, _ := NewScheduleStore(tmp.Name())
	store.Add("abc1", "cmd1", time.Time{}, "") //nolint:errcheck

	// Cancel using full ID that contains the short ID
	n := store.CancelBySession("hostname-abc1")
	if n != 1 {
		t.Errorf("expected 1 cancelled via suffix match, got %d", n)
	}
}

func TestDelete(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) //nolint:errcheck
	defer os.Remove(tmp.Name()) //nolint:errcheck

	store, _ := NewScheduleStore(tmp.Name())
	sc, _ := store.Add("sess1", "cmd1", time.Time{}, "")

	// Cancel it first (simulates done/cancelled state)
	store.Cancel(sc.ID) //nolint:errcheck

	// Delete should remove it entirely
	err := store.Delete(sc.ID)
	if err != nil {
		t.Errorf("expected delete to succeed, got: %v", err)
	}

	// Verify it's gone
	all := store.List()
	if len(all) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(all))
	}
}

func TestAddSpawn(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) //nolint:errcheck
	defer os.Remove(tmp.Name()) //nolint:errcheck

	store, err := NewScheduleStore(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	runAt := time.Now().Add(time.Hour)
	sc, err := store.AddSpawn(AddSpawnOptions{
		Task:       "run audit",
		ProjectDir: "/home/user/project",
		Backend:    "openai",
		LLMRef:     "gpt4o",
		Model:      "gpt-4o",
		Effort:     "thorough",
		SessionName: "audit-session",
		ScheduleName: "hourly-audit",
		RunAt:      runAt,
		OneShot:    true,
		Ephemeral:  true,
	})
	if err != nil {
		t.Fatalf("AddSpawn: %v", err)
	}

	if sc.Type != SchedTypeSpawn {
		t.Errorf("Type = %q, want %q", sc.Type, SchedTypeSpawn)
	}
	if sc.State != SchedPending {
		t.Errorf("State = %q, want %q", sc.State, SchedPending)
	}
	if sc.DeferredSession == nil {
		t.Fatal("DeferredSession is nil")
	}
	ds := sc.DeferredSession
	if ds.Task != "run audit" {
		t.Errorf("Task = %q, want %q", ds.Task, "run audit")
	}
	if ds.LLMRef != "gpt4o" {
		t.Errorf("LLMRef = %q, want %q", ds.LLMRef, "gpt4o")
	}
	if ds.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", ds.Model, "gpt-4o")
	}
	if ds.Effort != "thorough" {
		t.Errorf("Effort = %q, want %q", ds.Effort, "thorough")
	}
	if !ds.OneShot {
		t.Error("OneShot should be true")
	}
	if !ds.Ephemeral {
		t.Error("Ephemeral should be true")
	}

	// Reload from disk to verify persistence
	store2, _ := NewScheduleStore(tmp.Name())
	all := store2.List()
	if len(all) != 1 {
		t.Fatalf("expected 1 entry after reload, got %d", len(all))
	}
	reloaded := all[0]
	if reloaded.Type != SchedTypeSpawn {
		t.Errorf("reloaded Type = %q, want %q", reloaded.Type, SchedTypeSpawn)
	}
	if reloaded.DeferredSession == nil || reloaded.DeferredSession.LLMRef != "gpt4o" {
		t.Errorf("reloaded LLMRef mismatch")
	}
}

func TestDuePendingSessionsSpawn(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) //nolint:errcheck
	defer os.Remove(tmp.Name()) //nolint:errcheck

	store, _ := NewScheduleStore(tmp.Name())

	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)

	// Past spawn — should be returned as due
	store.AddSpawn(AddSpawnOptions{Task: "past-spawn", RunAt: past, OneShot: true}) //nolint:errcheck
	// Future spawn — not yet due
	store.AddSpawn(AddSpawnOptions{Task: "future-spawn", RunAt: future, OneShot: true}) //nolint:errcheck
	// Past new_session — also returned
	store.AddDeferredSession("ns", "ns-task", "/tmp", "", past) //nolint:errcheck
	// Regular command — should NOT be returned by DuePendingSessions
	store.Add("some-session", "plain-cmd", past, "") //nolint:errcheck

	due := store.DuePendingSessions(time.Now())
	if len(due) != 2 {
		t.Fatalf("expected 2 due sessions (1 spawn + 1 new_session), got %d", len(due))
	}

	types := map[string]int{}
	for _, d := range due {
		types[d.Type]++
	}
	if types[SchedTypeSpawn] != 1 {
		t.Errorf("expected 1 spawn in due list, got %d", types[SchedTypeSpawn])
	}
	if types[SchedTypeNewSession] != 1 {
		t.Errorf("expected 1 new_session in due list, got %d", types[SchedTypeNewSession])
	}
}

func TestAddSpawnCronNextFire(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) //nolint:errcheck
	defer os.Remove(tmp.Name()) //nolint:errcheck

	store, _ := NewScheduleStore(tmp.Name())

	// every minute cron; RunAt should be set to next minute
	sc, err := store.AddSpawn(AddSpawnOptions{
		Task:     "cron-spawn",
		CronExpr: "* * * * *",
		OneShot:  false,
	})
	if err != nil {
		t.Fatalf("AddSpawn with cron: %v", err)
	}
	if sc.RunAt.IsZero() {
		t.Error("expected RunAt to be set from cron, got zero")
	}
	if sc.RunAt.Before(time.Now()) {
		t.Error("expected RunAt to be in the future")
	}
}

func TestMarkDoneSpawnCronRecurrence(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) //nolint:errcheck
	defer os.Remove(tmp.Name()) //nolint:errcheck

	store, _ := NewScheduleStore(tmp.Name())

	sc, _ := store.AddSpawn(AddSpawnOptions{
		Task:     "recurring",
		CronExpr: "* * * * *",
		OneShot:  false,
	})

	// Simulate: mark done — cron recurrence should bump RunAt, keep pending
	err := store.MarkDone(sc.ID, false)
	if err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	updated, ok := store.Get(sc.ID)
	if !ok {
		t.Fatal("entry gone after MarkDone")
	}
	if updated.State != SchedPending {
		t.Errorf("State = %q after cron recurrence, want %q", updated.State, SchedPending)
	}
	if updated.RunAt.IsZero() || !updated.RunAt.After(time.Now().Add(-time.Second)) {
		t.Errorf("RunAt not updated after cron recurrence: %v", updated.RunAt)
	}
}
