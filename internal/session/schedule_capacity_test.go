package session

import (
	"context"
	"testing"
	"time"
)

// fillSessionCap saves n running sessions on the manager's host so the next
// Start is refused with "max sessions (n) reached".
func fillSessionCap(t *testing.T, mgr *Manager, n int) {
	t.Helper()
	mgr.SetMaxSessions(n)
	now := time.Now()
	for i := 0; i < n; i++ {
		id := string(rune('a'+i)) + "01"
		if err := mgr.store.Save(&Session{
			ID: id, FullID: "testhost-" + id, Hostname: "testhost",
			Name: "cap-filler", State: StateRunning, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// The detector schedules stopped for good when the session cap of 20 was hit:
// the manager's own schedule loop marked a recurring spawn failed on the
// refused start. A full session cap is a wait, never a failure.
func TestManagerScheduleLoop_SessionCapKeepsRecurringSchedulePending(t *testing.T) {
	mgr := newTestManager(t)
	store, err := NewScheduleStore(t.TempDir() + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetScheduleStore(store)
	fillSessionCap(t, mgr, 2)

	sc, err := store.AddSpawn(AddSpawnOptions{
		Task: "true", ScheduleName: "detector", CronExpr: "*/5 * * * *",
		Subprocess: true, OneShot: true, RunAt: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}

	mgr.processScheduledItems(context.Background())

	got, _ := store.Get(sc.ID)
	if got.State != SchedPending {
		t.Fatalf("a refused start on a full session cap must leave the recurring schedule pending, got state=%s", got.State)
	}
	if !got.RunAt.After(time.Now()) {
		t.Fatalf("the schedule must be re-armed for a future time, got run_at=%v", got.RunAt)
	}
}

// A schedule must keep its entries, fire counters and re-arm state across a
// daemon restart: stop the store, start it again from the same file.
func TestScheduleStore_RestartKeepsSchedules(t *testing.T) {
	path := t.TempDir() + "/schedule.json"
	store, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	cron, err := store.AddSpawn(AddSpawnOptions{Task: "scan", ScheduleName: "detector", CronExpr: "*/5 * * * *", Subprocess: true, OneShot: true})
	if err != nil {
		t.Fatal(err)
	}
	once, err := store.AddSpawn(AddSpawnOptions{Task: "once", ScheduleName: "one-time", RunAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.AddSpawn(AddSpawnOptions{Task: "gone", ScheduleName: "cancelled", CronExpr: "0 * * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel(cancelled.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordFire(cron.ID, "testhost-s1", "spawned"); err != nil {
		t.Fatal(err)
	}
	wantRunAt, _ := store.Get(cron.ID)
	wantNext := wantRunAt.RunAt

	// Restart: a brand-new store over the same file.
	reopened, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(reopened.List()); n != 3 {
		t.Fatalf("restart lost schedules: want 3 entries, got %d", n)
	}
	got, ok := reopened.Get(cron.ID)
	if !ok || got.State != SchedPending || got.CronExpr != "*/5 * * * *" || got.FireCount != 1 ||
		got.LastFireResult != "spawned" || got.ActiveSpawnID != "testhost-s1" || !got.RunAt.Equal(wantNext) {
		t.Fatalf("recurring schedule changed across restart: %+v", got)
	}
	if o, ok := reopened.Get(once.ID); !ok || o.State != SchedPending || o.DeferredSession == nil || o.DeferredSession.Task != "once" {
		t.Fatalf("one-time schedule changed across restart: %+v", o)
	}
	if c, ok := reopened.Get(cancelled.ID); !ok || c.State != SchedCancelled {
		t.Fatalf("cancelled schedule must stay cancelled across restart: %+v", c)
	}
	// Still due after the restart: an entry whose fire time passed while the
	// daemon was down fires once, not once per missed slot.
	if _, err := reopened.AddSpawn(AddSpawnOptions{Task: "late", ScheduleName: "late", CronExpr: "*/5 * * * *", RunAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	again, err := NewScheduleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if due := again.DuePendingSessions(time.Now()); len(due) != 1 || due[0].ScheduleName != "late" {
		t.Fatalf("an overdue schedule must be due once after restart, got %d", len(due))
	}
}
