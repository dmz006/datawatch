// BL359 — RestartSession from any state tests.

package session

import (
	"context"
	"testing"
	"time"
)

// TestBL359_RestartRunning verifies that a running session can be restarted:
// the underlying tmux session is killed and the session is relaunched.
func TestBL359_RestartRunning(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	sess := &Session{
		ID:          "r001",
		FullID:      "testhost-r001",
		Hostname:    "testhost",
		TmuxSession: "cs-r001",
		State:       StateRunning,
		Task:        "run tests",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		LogFile:     t.TempDir() + "/r001.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	updated, err := mgr.RestartSession(context.Background(), sess.FullID, "")
	if err != nil {
		t.Fatalf("RestartSession: %v", err)
	}

	// Session should now be running (or at least not in the original running state
	// stuck — the fake tmux new+pipe must have been called).
	if updated.FullID != sess.FullID {
		t.Errorf("FullID changed: got %q want %q", updated.FullID, sess.FullID)
	}
	if updated.State != StateRunning {
		t.Errorf("expected state running after restart, got %q", updated.State)
	}
	// The kill call must have been issued for the original tmux session.
	if fake.Count("kill") == 0 {
		t.Errorf("expected at least one tmux kill call, got none; calls: %+v", fake.Calls)
	}
	// Task should be preserved.
	if updated.Task != "run tests" {
		t.Errorf("task changed unexpectedly: got %q", updated.Task)
	}
}

// TestBL359_RestartFailed verifies that a failed session can be restarted.
func TestBL359_RestartFailed(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	sess := &Session{
		ID:          "f001",
		FullID:      "testhost-f001",
		Hostname:    "testhost",
		TmuxSession: "cs-f001",
		State:       StateFailed,
		Task:        "broken task",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		LogFile:     t.TempDir() + "/f001.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	updated, err := mgr.RestartSession(context.Background(), sess.FullID, "")
	if err != nil {
		t.Fatalf("RestartSession (failed): %v", err)
	}
	if updated.State != StateRunning {
		t.Errorf("expected state running after restart of failed session, got %q", updated.State)
	}
	if updated.FullID != sess.FullID {
		t.Errorf("FullID changed: got %q want %q", updated.FullID, sess.FullID)
	}
}

// TestBL359_RestartWithNewTask verifies that supplying a non-empty task overrides
// the session's existing task.
func TestBL359_RestartWithNewTask(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	sess := &Session{
		ID:          "t001",
		FullID:      "testhost-t001",
		Hostname:    "testhost",
		TmuxSession: "cs-t001",
		State:       StateComplete,
		Task:        "old task",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		LogFile:     t.TempDir() + "/t001.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	updated, err := mgr.RestartSession(context.Background(), sess.FullID, "new task")
	if err != nil {
		t.Fatalf("RestartSession (new task): %v", err)
	}
	if updated.Task != "new task" {
		t.Errorf("task not overridden: got %q want %q", updated.Task, "new task")
	}
}

// TestBL359_RestartUnknownID verifies that RestartSession returns an error for
// an unknown session ID.
func TestBL359_RestartUnknownID(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	_, err := mgr.RestartSession(context.Background(), "testhost-doesnotexist", "")
	if err == nil {
		t.Fatal("expected error for unknown session ID, got nil")
	}
}

// TestBL359_RestartPreservesName verifies that the session ID and Name are
// preserved after a restart.
func TestBL359_RestartPreservesName(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	sess := &Session{
		ID:          "n001",
		FullID:      "testhost-n001",
		Hostname:    "testhost",
		TmuxSession: "cs-n001",
		Name:        "my-named-session",
		State:       StateKilled,
		Task:        "do something",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		LogFile:     t.TempDir() + "/n001.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	updated, err := mgr.RestartSession(context.Background(), sess.FullID, "")
	if err != nil {
		t.Fatalf("RestartSession: %v", err)
	}
	if updated.ID != sess.ID {
		t.Errorf("session ID changed: got %q want %q", updated.ID, sess.ID)
	}
	if updated.FullID != sess.FullID {
		t.Errorf("session FullID changed: got %q want %q", updated.FullID, sess.FullID)
	}
	if updated.Name != sess.Name {
		t.Errorf("session Name changed: got %q want %q", updated.Name, sess.Name)
	}
}
