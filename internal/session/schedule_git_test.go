package session

import (
	"path/filepath"
	"testing"
	"time"
)

// TestSpawnGit_RoundTrip verifies the Git field survives a persist + reload
// cycle on a spawn schedule (GH#128 spawn path stores Git in DeferredSession).
func TestSpawnGit_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedule.json")

	store, err := NewScheduleStore(path)
	if err != nil {
		t.Fatalf("NewScheduleStore: %v", err)
	}
	sc, err := store.AddSpawn(AddSpawnOptions{
		Task:       "do the thing",
		RunAt:      time.Now().Add(time.Hour),
		OneShot:    true,
		Subprocess: true,
		Git:        true,
	})
	if err != nil {
		t.Fatalf("AddSpawn: %v", err)
	}
	if sc.DeferredSession == nil {
		t.Fatal("expected DeferredSession to be set")
	}
	if !sc.DeferredSession.Git {
		t.Fatalf("expected Git=true on DeferredSession, got %v", sc.DeferredSession.Git)
	}

	// Reload from disk in a fresh store; Git must be preserved.
	reloaded, err := NewScheduleStore(path)
	if err != nil {
		t.Fatalf("reload NewScheduleStore: %v", err)
	}
	got, ok := reloaded.Get(sc.ID)
	if !ok {
		t.Fatalf("schedule entry %s not found after reload", sc.ID)
	}
	if got.DeferredSession == nil {
		t.Fatal("DeferredSession lost on reload")
	}
	if !got.DeferredSession.Git {
		t.Errorf("Git not preserved across reload: got %v, want true", got.DeferredSession.Git)
	}
	if !got.DeferredSession.Subprocess {
		t.Errorf("Subprocess not preserved across reload (sanity): got %v", got.DeferredSession.Subprocess)
	}
}

// TestSpawnGit_DefaultFalse verifies Git defaults to false when unset, so
// existing spawn schedules that never opted in keep the no-git behavior.
func TestSpawnGit_DefaultFalse(t *testing.T) {
	_, sc := newSpawnSched(t, AddSpawnOptions{Task: "x", RunAt: time.Now().Add(time.Hour)})
	if sc.DeferredSession == nil {
		t.Fatal("expected DeferredSession to be set")
	}
	if sc.DeferredSession.Git {
		t.Errorf("expected Git=false by default, got true")
	}
}
