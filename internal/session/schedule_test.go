package session

import (
	"os"
	"testing"
	"time"
)

func TestCancelBySession(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name()) // Remove so NewScheduleStore creates fresh
	defer os.Remove(tmp.Name())

	store, err := NewScheduleStore(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Add commands for two sessions
	store.Add("host-abc1", "cmd1", time.Time{}, "")
	store.Add("host-abc1", "cmd2", time.Now().Add(time.Hour), "")
	store.Add("host-def2", "cmd3", time.Time{}, "")

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
	os.Remove(tmp.Name())
	defer os.Remove(tmp.Name())

	store, _ := NewScheduleStore(tmp.Name())
	store.Add("abc1", "cmd1", time.Time{}, "")

	// Cancel using full ID that contains the short ID
	n := store.CancelBySession("hostname-abc1")
	if n != 1 {
		t.Errorf("expected 1 cancelled via suffix match, got %d", n)
	}
}

func TestDelete(t *testing.T) {
	tmp, _ := os.CreateTemp("", "sched-test-*.json")
	os.Remove(tmp.Name())
	defer os.Remove(tmp.Name())

	store, _ := NewScheduleStore(tmp.Name())
	sc, _ := store.Add("sess1", "cmd1", time.Time{}, "")

	// Cancel it first (simulates done/cancelled state)
	store.Cancel(sc.ID)

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
