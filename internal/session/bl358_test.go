// BL358 — Discussion subscribe store unit tests.

package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func newTestSubStore(t *testing.T) (*session.DiscussionSubStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "discussion_subs.json")
	store, err := session.NewDiscussionSubStore(path)
	if err != nil {
		t.Fatalf("NewDiscussionSubStore: %v", err)
	}
	return store, path
}

// TestBL358_Subscribe verifies that subscribing two sessions to a discussion
// returns both session names from GetSubscribers.
func TestBL358_Subscribe(t *testing.T) {
	store, _ := newTestSubStore(t)

	if err := store.Subscribe("sprint-42", "alice"); err != nil {
		t.Fatalf("Subscribe alice: %v", err)
	}
	if err := store.Subscribe("sprint-42", "bob"); err != nil {
		t.Fatalf("Subscribe bob: %v", err)
	}

	subs := store.GetSubscribers("sprint-42")
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscribers, got %d: %v", len(subs), subs)
	}
	seen := make(map[string]bool)
	for _, s := range subs {
		seen[s] = true
	}
	if !seen["alice"] {
		t.Errorf("alice not found in subscribers: %v", subs)
	}
	if !seen["bob"] {
		t.Errorf("bob not found in subscribers: %v", subs)
	}
}

// TestBL358_SubscribeIdempotent verifies that subscribing the same session twice
// results in exactly one entry (no duplicate).
func TestBL358_SubscribeIdempotent(t *testing.T) {
	store, _ := newTestSubStore(t)

	if err := store.Subscribe("my-disc", "session-a"); err != nil {
		t.Fatalf("first Subscribe: %v", err)
	}
	if err := store.Subscribe("my-disc", "session-a"); err != nil {
		t.Fatalf("second Subscribe (idempotent): %v", err)
	}

	subs := store.GetSubscribers("my-disc")
	if len(subs) != 1 {
		t.Fatalf("expected exactly 1 subscriber after idempotent Subscribe, got %d: %v", len(subs), subs)
	}
}

// TestBL358_Unsubscribe verifies that subscribing then unsubscribing leaves
// GetSubscribers returning an empty slice.
func TestBL358_Unsubscribe(t *testing.T) {
	store, _ := newTestSubStore(t)

	if err := store.Subscribe("incident-2026", "monitor"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := store.Unsubscribe("incident-2026", "monitor"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	subs := store.GetSubscribers("incident-2026")
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscribers after Unsubscribe, got %d: %v", len(subs), subs)
	}
}

// TestBL358_Persistence verifies that subscriptions survive a store reload
// from the same file.
func TestBL358_Persistence(t *testing.T) {
	store, path := newTestSubStore(t)

	if err := store.Subscribe("persist-disc", "session-x"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Verify the file was written.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file not created: %v", err)
	}

	// Reload from same path.
	store2, err := session.NewDiscussionSubStore(path)
	if err != nil {
		t.Fatalf("reload NewDiscussionSubStore: %v", err)
	}

	subs := store2.GetSubscribers("persist-disc")
	if len(subs) != 1 || subs[0] != "session-x" {
		t.Fatalf("expected [session-x] after reload, got %v", subs)
	}
}

// TestBL358_List verifies that subscribing to multiple discussions returns all
// subs from List().
func TestBL358_List(t *testing.T) {
	store, _ := newTestSubStore(t)

	if err := store.Subscribe("disc-a", "sess-1"); err != nil {
		t.Fatalf("Subscribe disc-a/sess-1: %v", err)
	}
	if err := store.Subscribe("disc-b", "sess-2"); err != nil {
		t.Fatalf("Subscribe disc-b/sess-2: %v", err)
	}
	if err := store.Subscribe("disc-a", "sess-3"); err != nil {
		t.Fatalf("Subscribe disc-a/sess-3: %v", err)
	}

	all := store.List()
	if len(all) != 3 {
		t.Fatalf("expected 3 subs from List, got %d: %v", len(all), all)
	}
}
