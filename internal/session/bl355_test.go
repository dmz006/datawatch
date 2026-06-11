// BL355 — claude_alive zombie detection tests.

package session

import (
	"context"
	"testing"
	"time"
)

// TestBL355_ProbeClaudeAlive_NoTmux verifies that a session with an empty
// TmuxSession is always considered alive (virtual/council/agent session).
func TestBL355_ProbeClaudeAlive_NoTmux(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	sess := &Session{
		ID:          "a001",
		FullID:      "testhost-a001",
		Hostname:    "testhost",
		TmuxSession: "", // virtual session — no pane
		State:       StateRunning,
	}

	alive := mgr.probeClaudeAlive(sess)
	if !alive {
		t.Errorf("probeClaudeAlive: virtual session (empty TmuxSession) should return true, got false")
	}
}

// TestBL355_ClaudeAlive_FieldPersists verifies that the ClaudeAlive field
// round-trips through the store (JSON marshal/unmarshal).
func TestBL355_ClaudeAlive_FieldPersists(t *testing.T) {
	mgr := newTestManager(t)
	trueVal := true

	sess := &Session{
		ID:          "b001",
		FullID:      "testhost-b001",
		Hostname:    "testhost",
		State:       StateRunning,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ClaudeAlive: &trueVal,
	}

	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, ok := mgr.store.Get("testhost-b001")
	if !ok {
		t.Fatal("session not found after save")
	}
	if loaded.ClaudeAlive == nil {
		t.Fatal("ClaudeAlive is nil after reload — field not persisted")
	}
	if !*loaded.ClaudeAlive {
		t.Errorf("ClaudeAlive persisted as false, expected true")
	}
}

// TestBL355_ClaudeAlive_FalseFieldPersists verifies that ClaudeAlive=false
// also round-trips correctly (not omitted by omitempty since it is a pointer).
func TestBL355_ClaudeAlive_FalseFieldPersists(t *testing.T) {
	mgr := newTestManager(t)
	falseVal := false

	sess := &Session{
		ID:          "c001",
		FullID:      "testhost-c001",
		Hostname:    "testhost",
		State:       StateRunning,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ClaudeAlive: &falseVal,
	}

	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, ok := mgr.store.Get("testhost-c001")
	if !ok {
		t.Fatal("session not found after save")
	}
	if loaded.ClaudeAlive == nil {
		t.Fatal("ClaudeAlive is nil after reload — false pointer not persisted")
	}
	if *loaded.ClaudeAlive {
		t.Errorf("ClaudeAlive persisted as true, expected false")
	}
}

// TestBL355_ClaudeAlive_NilForInactive verifies that ClaudeAlive is nil
// for inactive sessions (the reconciler should clear it).
func TestBL355_ClaudeAlive_NilForInactive(t *testing.T) {
	mgr := newTestManager(t)

	// Start with a complete session that has ClaudeAlive set (stale probe)
	staleTrue := true
	sess := &Session{
		ID:          "d001",
		FullID:      "testhost-d001",
		Hostname:    "testhost",
		State:       StateComplete,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ClaudeAlive: &staleTrue,
	}

	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Manually invoke the clearing logic that reconcileSessions does for
	// inactive sessions: clear ClaudeAlive and re-save.
	loaded, ok := mgr.store.Get("testhost-d001")
	if !ok {
		t.Fatal("session not found")
	}
	if loaded.ClaudeAlive == nil {
		t.Fatal("precondition: ClaudeAlive should be set before clearing")
	}
	loaded.ClaudeAlive = nil
	if err := mgr.store.Save(loaded); err != nil {
		t.Fatalf("Save cleared: %v", err)
	}

	reloaded, ok := mgr.store.Get("testhost-d001")
	if !ok {
		t.Fatal("session not found after clearing")
	}
	if reloaded.ClaudeAlive != nil {
		t.Errorf("expected ClaudeAlive nil for inactive session, got %v", *reloaded.ClaudeAlive)
	}
}

// TestBL355_ReconcileZombieDetection verifies that reconcileSessions fires
// onClaudeAliveChange when a session transitions from alive→dead.
// Uses probeClaudeAliveFunc injection so the test runs without a real tmux.
func TestBL355_ReconcileZombieDetection(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	// Register a tmux session so SessionExists returns true for this session.
	tmuxName := "cs-e001"
	_ = fake.NewSessionWithSize(tmuxName, 80, 24)

	aliveTrue := true
	sess := &Session{
		ID:          "e001",
		FullID:      "testhost-e001",
		Hostname:    "testhost",
		TmuxSession: tmuxName,
		State:       StateRunning,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ClaudeAlive: &aliveTrue, // previously probed as alive
	}

	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Wire callback to detect the alive→dead flip.
	var zombieFired bool
	var zombieSessID string
	mgr.SetClaudeAliveChangeHandler(func(s *Session) {
		zombieFired = true
		zombieSessID = s.FullID
	})

	// Inject probe that returns false (simulates Claude process having exited).
	mgr.probeClaudeAliveFunc = func(s *Session) bool { return false }

	// Call the real reconcileSessions — the BL355 block should detect the
	// alive(true)→dead(false) flip and fire onClaudeAliveChange.
	mgr.reconcileSessions(context.Background())

	if !zombieFired {
		t.Error("expected onClaudeAliveChange to fire on alive→dead transition")
	}
	if zombieSessID != "testhost-e001" {
		t.Errorf("zombieSessID = %q, want testhost-e001", zombieSessID)
	}

	// Verify ClaudeAlive was updated to false in the store.
	reloaded, ok := mgr.store.Get("testhost-e001")
	if !ok {
		t.Fatal("session not found after reconcile")
	}
	if reloaded.ClaudeAlive == nil {
		t.Fatal("ClaudeAlive is nil after reconcile — should be false")
	}
	if *reloaded.ClaudeAlive {
		t.Errorf("ClaudeAlive = true after probe returned false — reconciler didn't update")
	}
}

// TestBL355_ReconcileClearsAliveOnTermination verifies that ClaudeAlive is
// cleared (nil) when the reconciler marks a session StateComplete due to tmux gone.
func TestBL355_ReconcileClearsAliveOnTermination(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t) // no fake tmux session registered → SessionExists returns false

	aliveTrue := true
	sess := &Session{
		ID:          "f001",
		FullID:      "testhost-f001",
		Hostname:    "testhost",
		TmuxSession: "cs-f001", // fake tmux does NOT have this → SessionExists = false
		State:       StateRunning,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ClaudeAlive: &aliveTrue,
	}

	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Inject probe to prevent real tmux calls (tmux not alive anyway, so branch won't reach probe).
	mgr.probeClaudeAliveFunc = func(s *Session) bool { return true }

	// reconcileSessions: tmux not alive → isActive && !tmuxAlive → StateComplete.
	// BL355 fix: ClaudeAlive must be cleared before saving.
	mgr.reconcileSessions(context.Background())

	reloaded, ok := mgr.store.Get("testhost-f001")
	if !ok {
		t.Fatal("session not found after reconcile")
	}
	if reloaded.State != StateComplete {
		t.Errorf("state = %v, want StateComplete", reloaded.State)
	}
	if reloaded.ClaudeAlive != nil {
		t.Errorf("ClaudeAlive should be nil after StateComplete, got %v", *reloaded.ClaudeAlive)
	}
}
