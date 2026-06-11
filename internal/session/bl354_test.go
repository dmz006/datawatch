// BL354 — FindSessionByName tie-breaking tests.

package session

import (
	"testing"
	"time"
)

// TestBL354_FindSessionByName_PrefersActive verifies that active (running/waiting_input)
// sessions are returned before done/stopped sessions, and among active sessions the
// oldest (earliest CreatedAt) wins.
func TestBL354_FindSessionByName_PrefersActive(t *testing.T) {
	mgr := newTestManager(t)

	now := time.Now()

	// One completed session (old) and one running session (new) — running should win.
	mgr.store.Save(&Session{ //nolint:errcheck
		ID: "d01", FullID: "testhost-d01", Hostname: "testhost",
		Name: "prefer-active", State: StateComplete,
		CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour),
	})
	mgr.store.Save(&Session{ //nolint:errcheck
		ID: "r01", FullID: "testhost-r01", Hostname: "testhost",
		Name: "prefer-active", State: StateRunning,
		CreatedAt: now, UpdatedAt: now,
	})

	sess, ok := mgr.FindSessionByName("prefer-active")
	if !ok {
		t.Fatal("expected a match, got none")
	}
	if sess.ID != "r01" {
		t.Errorf("expected running session r01, got %q", sess.ID)
	}
}

// TestBL354_FindSessionByName_OldestAmongActive verifies that among multiple
// active sessions with the same name, the oldest (earliest CreatedAt) is returned.
func TestBL354_FindSessionByName_OldestAmongActive(t *testing.T) {
	mgr := newTestManager(t)

	now := time.Now()

	// Three active sessions — oldest should win.
	for _, tc := range []struct {
		id        string
		createdAt time.Time
	}{
		{"r02", now.Add(-5 * time.Minute)},  // second oldest
		{"r01", now.Add(-10 * time.Minute)}, // oldest — should win
		{"r03", now},                          // newest
	} {
		mgr.store.Save(&Session{ //nolint:errcheck
			ID: tc.id, FullID: "testhost-" + tc.id, Hostname: "testhost",
			Name: "race-condition", State: StateRunning,
			CreatedAt: tc.createdAt, UpdatedAt: tc.createdAt,
		})
	}

	sess, ok := mgr.FindSessionByName("race-condition")
	if !ok {
		t.Fatal("expected a match, got none")
	}
	if sess.ID != "r01" {
		t.Errorf("expected oldest session r01, got %q", sess.ID)
	}
}

// TestBL354_FindSessionByName_WaitingInputIsActive verifies that StateWaitingInput
// is treated as active for tie-breaking purposes.
func TestBL354_FindSessionByName_WaitingInputIsActive(t *testing.T) {
	mgr := newTestManager(t)

	now := time.Now()

	mgr.store.Save(&Session{ //nolint:errcheck
		ID: "d02", FullID: "testhost-d02", Hostname: "testhost",
		Name: "waiting-wins", State: StateComplete,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})
	mgr.store.Save(&Session{ //nolint:errcheck
		ID: "w01", FullID: "testhost-w01", Hostname: "testhost",
		Name: "waiting-wins", State: StateWaitingInput,
		CreatedAt: now, UpdatedAt: now,
	})

	sess, ok := mgr.FindSessionByName("waiting-wins")
	if !ok {
		t.Fatal("expected a match, got none")
	}
	if sess.ID != "w01" {
		t.Errorf("expected waiting-input session w01, got %q", sess.ID)
	}
}

// TestBL354_FindSessionByName_AllDone_ReturnsMostRecentlyUpdated verifies that
// when all matched sessions are done, the first match (as sorted) is returned.
// The sort for done sessions is by oldest CreatedAt (same behaviour — consistent).
func TestBL354_FindSessionByName_AllDone(t *testing.T) {
	mgr := newTestManager(t)

	now := time.Now()

	mgr.store.Save(&Session{ //nolint:errcheck
		ID: "e01", FullID: "testhost-e01", Hostname: "testhost",
		Name: "all-done", State: StateComplete,
		CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour),
	})
	mgr.store.Save(&Session{ //nolint:errcheck
		ID: "e02", FullID: "testhost-e02", Hostname: "testhost",
		Name: "all-done", State: StateComplete,
		CreatedAt: now, UpdatedAt: now,
	})

	sess, ok := mgr.FindSessionByName("all-done")
	if !ok {
		t.Fatal("expected a match, got none")
	}
	// Both are done (inactive), so sort by oldest first → e01.
	if sess.ID != "e01" {
		t.Errorf("expected oldest session e01, got %q", sess.ID)
	}
}

// TestBL354_FindSessionByName_NotFound verifies (nil, false) when no sessions match.
func TestBL354_FindSessionByName_NotFound(t *testing.T) {
	mgr := newTestManager(t)

	sess, ok := mgr.FindSessionByName("does-not-exist")
	if ok || sess != nil {
		t.Errorf("expected (nil, false), got (%v, %v)", sess, ok)
	}
}
