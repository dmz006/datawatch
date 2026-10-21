package session

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// TestSessionScheduleNamePropagatesFromAddSpawn (GH#204 Phase 1) — the
// schedule entry's name must land in the DeferredSession at AddSpawn time
// and survive the save/load round trip, so a daemon restart (or the manager
// picking up a schedule it did not create itself) still knows which
// schedule will own the spawned session.
func TestSessionScheduleNamePropagatesFromAddSpawn(t *testing.T) {
	path := t.TempDir() + "/schedule.json"
	store, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := store.AddSpawn(AddSpawnOptions{
		Task:         "run the report",
		CronExpr:     "0 * * * *",
		ScheduleName: "nightly-report",
		Subprocess:   true,
		OneShot:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.DeferredSession == nil {
		t.Fatal("spawn schedule must carry a DeferredSession")
	}
	if sc.DeferredSession.ScheduleName != "nightly-report" {
		t.Fatalf("deferred.ScheduleName = %q, want nightly-report", sc.DeferredSession.ScheduleName)
	}

	// Reload from disk: the name must survive the persistence round trip.
	reloaded, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(sc.ID)
	if !ok || got == nil {
		t.Fatalf("reload Get(%s): not found", sc.ID)
	}
	if got.DeferredSession == nil || got.DeferredSession.ScheduleName != "nightly-report" {
		t.Fatalf("after save/load, deferred.ScheduleName = %+v, want nightly-report", got.DeferredSession)
	}
}

// TestSessionScheduleNameAttachedToSpawnedSession (GH#204 Phase 1) —
// end-to-end through the manager's schedule loop: when a due spawn
// schedule fires, the resulting Session must have ScheduleName set
// server-side (from the DeferredSession, with the entry's own ScheduleName
// as fallback) and persisted with the session record.
func TestSessionScheduleNameAttachedToSpawnedSession(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not available: %v", err)
	}
	dataDir := t.TempDir()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetConfig(config.DefaultConfig())

	schedStore, err := NewScheduleStore(dataDir + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(schedStore)

	sc, err := schedStore.AddSpawn(AddSpawnOptions{
		Task:         "true",
		ScheduleName: "nightly-report",
		Subprocess:   true,
		OneShot:      true,
		RunAt:        time.Now().Add(-time.Second), // due now
	})
	if err != nil {
		t.Fatal(err)
	}

	before := len(mgr.store.List())
	mgr.processScheduledItems(context.Background())
	after := mgr.store.List()
	if len(after) != before+1 {
		t.Fatalf("schedule fire should have spawned exactly one session; had %d, now %d", before, len(after))
	}
	sess := after[before]

	// Let the spawned one-shot finish so the finisher goroutine has done its
	// last writes before TempDir cleanup runs (no half-written session dir
	// left behind at test end).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cur, ok := mgr.store.Get(sess.FullID); ok && (cur.State == StateComplete || cur.State == StateFailed) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cur, ok := mgr.store.Get(sess.FullID); !ok || cur.State != StateComplete {
		if cur != nil {
			t.Fatalf("spawned subprocess session should complete cleanly: state=%s", cur.State)
		}
		t.Fatal("spawned session vanished from the store")
	}

	if sess.ScheduleName != "nightly-report" {
		t.Fatalf("spawned session ScheduleName = %q, want nightly-report (server-side, from the schedule entry)", sess.ScheduleName)
	}

	// Persisted: the record on disk must carry it too.
	if cur, ok := mgr.store.Get(sess.FullID); !ok || cur.ScheduleName != "nightly-report" {
		t.Fatalf("stored session record must persist ScheduleName; got %+v", cur)
	}

	// The entry itself keeps its name for future fires.
	if got, ok := schedStore.Get(sc.ID); !ok || got.ScheduleName != "nightly-report" {
		t.Fatalf("schedule entry name lost after fire: ok=%v entry=%+v", ok, got)
	}
}
