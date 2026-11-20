package session

import (
	"context"
	"os/exec"
	"strings"
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
	t.Cleanup(func() { _ = mgr.Close() })
	allowTestTemp(mgr)
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

// TestSessionLaunchRolePropagatesFromAddSpawn — the launch role requested
// on the spawn schedule (e.g. the supervisor role for a lean recurring
// pass) must land in the DeferredSession at AddSpawn time, survive the
// save/load round trip, and be stamped on the spawned session by the
// manager's fire path.
func TestSessionLaunchRolePropagatesFromAddSpawn(t *testing.T) {
	path := t.TempDir() + "/schedule.json"
	store, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := store.AddSpawn(AddSpawnOptions{
		Task:         "run the recurring pass",
		ScheduleName: "supervisor-spawn-demo",
		LaunchRole:   "supervisor",
		Subprocess:   true,
		OneShot:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.DeferredSession == nil {
		t.Fatal("spawn schedule must carry a DeferredSession")
	}
	if sc.DeferredSession.LaunchRole != "supervisor" {
		t.Fatalf("deferred.LaunchRole = %q, want supervisor", sc.DeferredSession.LaunchRole)
	}

	// Reload from disk: the role must survive the persistence round trip.
	reloaded, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(sc.ID)
	if !ok || got == nil {
		t.Fatalf("reload Get(%s): not found", sc.ID)
	}
	if got.DeferredSession == nil || got.DeferredSession.LaunchRole != "supervisor" {
		t.Fatalf("after save/load, deferred.LaunchRole = %+v, want supervisor", got.DeferredSession)
	}

	// The default stays untagged: no LaunchRole means an ordinary session.
	sc2, err := store.AddSpawn(AddSpawnOptions{Task: "plain", Subprocess: true, OneShot: true})
	if err != nil {
		t.Fatal(err)
	}
	if sc2.DeferredSession.LaunchRole != "" {
		t.Fatalf("default LaunchRole = %q, want empty (untagged)", sc2.DeferredSession.LaunchRole)
	}
}

// TestSessionLaunchRoleAttachedToSpawnedSession — end-to-end through the
// manager's schedule loop: when a due spawn schedule fires with a launch
// role, the resulting Session must have LaunchRole set server-side and
// persisted with the session record.
func TestSessionLaunchRoleAttachedToSpawnedSession(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not available: %v", err)
	}
	dataDir := t.TempDir()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	allowTestTemp(mgr)
	mgr.SetConfig(config.DefaultConfig())

	schedStore, err := NewScheduleStore(dataDir + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(schedStore)

	sc, err := schedStore.AddSpawn(AddSpawnOptions{
		Task:         "true",
		ScheduleName: "supervisor-spawn-demo",
		LaunchRole:   "supervisor",
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
	// last writes before TempDir cleanup runs.
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

	if sess.LaunchRole != "supervisor" {
		t.Fatalf("spawned session LaunchRole = %q, want supervisor (server-side, from the schedule entry)", sess.LaunchRole)
	}

	// Persisted: the record on disk must carry it too.
	if cur, ok := mgr.store.Get(sess.FullID); !ok || cur.LaunchRole != "supervisor" {
		t.Fatalf("stored session record must persist LaunchRole; got %+v", cur)
	}

	// The entry itself keeps its role for future fires.
	if got, ok := schedStore.Get(sc.ID); !ok || got.DeferredSession == nil || got.DeferredSession.LaunchRole != "supervisor" {
		t.Fatalf("schedule entry role lost after fire: ok=%v entry=%+v", ok, got)
	}
}

// TestEnsureNamedSpawnIdempotent — a named spawn schedule is added at most
// once: the second ensure for the same name returns the existing pending
// entry instead of adding a duplicate, so set-up can run on every boot.
func TestEnsureNamedSpawnIdempotent(t *testing.T) {
	store, err := NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	name := "supervisor-spawn-demo"

	first, created, err := store.EnsureNamedSpawn(AddSpawnOptions{
		Task:         "run the recurring pass",
		CronExpr:     "*/20 * * * *",
		LaunchRole:   "supervisor",
		Subprocess:   true,
		OneShot:      true,
		ScheduleName: name,
	})
	if err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v, want created", created, err)
	}
	if first.ScheduleName != name {
		t.Fatalf("name = %q, want %q", first.ScheduleName, name)
	}

	second, created, err := store.EnsureNamedSpawn(AddSpawnOptions{
		Task:         "a different task for the same name",
		CronExpr:     "0 * * * *",
		LaunchRole:   "supervisor",
		Subprocess:   true,
		OneShot:      true,
		ScheduleName: name,
	})
	if err != nil || created {
		t.Fatalf("second ensure: created=%v err=%v, want the existing pending entry", created, err)
	}
	if second.ID != first.ID {
		t.Fatalf("second ensure returned %s, want the existing %s", second.ID, first.ID)
	}
	if second.DeferredSession.Task != "run the recurring pass" {
		t.Fatalf("second ensure changed the task to %q", second.DeferredSession.Task)
	}

	// A different name still adds its own schedule.
	other, created, err := store.EnsureNamedSpawn(AddSpawnOptions{Task: "other", Subprocess: true, OneShot: true, ScheduleName: "other-schedule"})
	if err != nil || !created || other.ID == first.ID {
		t.Fatalf("different name: created=%v err=%v id=%s, want a new entry", created, err, other.ID)
	}
}

// TestEnsureNamedSpawnRequiresName — a name is the idempotency key, so it
// is required.
func TestEnsureNamedSpawnRequiresName(t *testing.T) {
	store, err := NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.EnsureNamedSpawn(AddSpawnOptions{Task: "anon", Subprocess: true}); err == nil {
		t.Fatal("expected an error for a missing ScheduleName")
	} else if !strings.Contains(err.Error(), "ScheduleName") {
		t.Fatalf("error %q should name the missing ScheduleName", err)
	}
}

// TestEnsureNamedSpawnAfterDoneEntry — a schedule that already ran (done)
// does not block re-ensuring the same name: only a pending spawn under the
// name counts as the existing one.
func TestEnsureNamedSpawnAfterDoneEntry(t *testing.T) {
	store, err := NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	name := "supervisor-spawn-demo"

	first, err := store.AddSpawn(AddSpawnOptions{Task: "old", RunAt: time.Now().Add(time.Hour), Subprocess: true, OneShot: true, ScheduleName: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDone(first.ID, false); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	again, created, err := store.EnsureNamedSpawn(AddSpawnOptions{Task: "fresh", RunAt: time.Now().Add(time.Hour), Subprocess: true, OneShot: true, ScheduleName: name, LaunchRole: "supervisor"})
	if err != nil || !created {
		t.Fatalf("ensure after a done entry: created=%v err=%v, want a fresh pending entry", created, err)
	}
	if again.DeferredSession == nil || again.DeferredSession.Task != "fresh" {
		t.Fatalf("task = %+v, want fresh", again.DeferredSession)
	}
}
