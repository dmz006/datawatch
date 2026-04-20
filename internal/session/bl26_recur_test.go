// BL26 — recurring schedule tests.

package session

import (
	"testing"
	"time"
)

func TestBL26_MarkDone_RecurringReschedules(t *testing.T) {
	dir := t.TempDir()
	store, err := NewScheduleStore(dir + "/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	cmd := &ScheduledCommand{
		ID: "ab12cd34", SessionID: "h-aa", Command: "ping",
		RunAt: time.Now().Add(-time.Second), State: SchedPending,
		RecurEverySeconds: 60,
	}
	store.entries = append(store.entries, cmd)
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDone(cmd.ID, false); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(cmd.ID)
	if !ok {
		t.Fatal("entry vanished after MarkDone")
	}
	if got.State != SchedPending {
		t.Errorf("state=%q want pending after recurring success", got.State)
	}
	if got.RunAt.Before(time.Now()) {
		t.Errorf("RunAt should advance into the future, got %v", got.RunAt)
	}
}

func TestBL26_MarkDone_RecurringPastDeadlineCompletes(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewScheduleStore(dir + "/schedule.json")
	cmd := &ScheduledCommand{
		ID: "expired1", SessionID: "h-aa", Command: "ping",
		RunAt: time.Now().Add(-time.Second), State: SchedPending,
		RecurEverySeconds: 60,
		RecurUntil:        time.Now().Add(-time.Hour),
	}
	store.entries = append(store.entries, cmd); _ = store.save()
	_ = store.MarkDone(cmd.ID, false)
	got, _ := store.Get(cmd.ID)
	if got.State != SchedDone {
		t.Errorf("expected done after RecurUntil exceeded, got %q", got.State)
	}
}

func TestBL26_MarkDone_FailedRecurringStillFails(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewScheduleStore(dir + "/schedule.json")
	cmd := &ScheduledCommand{
		ID: "fail1", SessionID: "h-aa", Command: "ping",
		RunAt: time.Now(), State: SchedPending, RecurEverySeconds: 60,
	}
	store.entries = append(store.entries, cmd); _ = store.save()
	_ = store.MarkDone(cmd.ID, true)
	got, _ := store.Get(cmd.ID)
	if got.State != SchedFailed {
		t.Errorf("failed recurring should mark failed (no auto-recur), got %q", got.State)
	}
}
