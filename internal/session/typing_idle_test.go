// Tests for WaitTypingIdle and QueueChannelSend.
//
// Key correctness properties:
//  1. No operator keystrokes → WaitTypingIdle returns immediately (idle=0 < threshold).
//  2. Recent operator keystrokes → WaitTypingIdle holds until idleFor gap or deadline.
//  3. Session-to-session sends (no SendRawKeys) are not held — fixes the regression
//     where TTY mtime from AI output was mistakenly treated as "typing".

package session

import (
	"testing"
	"time"
)

// newTypingTestManager builds a minimal Manager suitable for WaitTypingIdle tests.
func newTypingTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager("test-host", dir, "", 30*time.Minute)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// TestWaitTypingIdle_NoActivity verifies that a session with no recent
// operator keystrokes is considered idle immediately (the common path for
// session-to-session communication where no human is present).
func TestWaitTypingIdle_NoActivity(t *testing.T) {
	m := newTypingTestManager(t)
	start := time.Now()
	// idleFor=1s but no activity recorded — should return true immediately.
	got := m.WaitTypingIdle("no-such-session", 1*time.Second, 5*time.Second)
	elapsed := time.Since(start)
	if !got {
		t.Error("WaitTypingIdle: want true (idle), got false (deadline)")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("WaitTypingIdle should return immediately when no activity; took %v", elapsed)
	}
}

// TestWaitTypingIdle_RecentTyping verifies that a session with a just-recorded
// raw keystroke is held until the idle gap or deadline fires.
func TestWaitTypingIdle_RecentTyping(t *testing.T) {
	m := newTypingTestManager(t)
	const sessID = "test-sess"

	// Simulate the operator just typed something.
	m.mu.Lock()
	m.lastRawInputAt[sessID] = time.Now()
	m.mu.Unlock()

	// idleFor=200ms, deadline=500ms — should return false (deadline) since we
	// record no further activity; the last-typing timestamp stays fresh for the
	// whole test because it won't be updated again.
	// Actually: time.Since(last) will grow as the test runs, so it will
	// reach 200ms naturally — return true.
	start := time.Now()
	got := m.WaitTypingIdle(sessID, 200*time.Millisecond, 2*time.Second)
	elapsed := time.Since(start)

	if !got {
		t.Error("WaitTypingIdle: want true (idle gap reached), got false (deadline)")
	}
	// Should have waited roughly 200ms (the idle gap).
	if elapsed < 150*time.Millisecond {
		t.Errorf("WaitTypingIdle returned too fast (%v) — should wait for idle gap", elapsed)
	}
}

// TestWaitTypingIdle_DeadlineExpires verifies the deadline path: if the
// operator keeps typing continuously, WaitTypingIdle gives up and returns
// false after the deadline so the message is never dropped.
func TestWaitTypingIdle_DeadlineExpires(t *testing.T) {
	m := newTypingTestManager(t)
	const sessID = "active-sess"

	// Prime the map so the initial check in WaitTypingIdle sees recent activity.
	m.mu.Lock()
	m.lastRawInputAt[sessID] = time.Now()
	m.mu.Unlock()

	// Keep refreshing to simulate continuous typing.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				m.mu.Lock()
				m.lastRawInputAt[sessID] = time.Now()
				m.mu.Unlock()
			}
		}
	}()
	defer close(stop)

	start := time.Now()
	// idleFor=500ms but typing keeps happening every 50ms → idle never reached.
	// deadline=300ms → should return false.
	got := m.WaitTypingIdle(sessID, 500*time.Millisecond, 300*time.Millisecond)
	elapsed := time.Since(start)

	if got {
		t.Error("WaitTypingIdle: want false (deadline), got true (idle)")
	}
	if elapsed < 250*time.Millisecond || elapsed > 600*time.Millisecond {
		t.Errorf("WaitTypingIdle deadline elapsed unexpected: %v", elapsed)
	}
}

// TestSendRawKeys_UpdatesLastRawInputAt verifies that SendRawKeys records
// the keystroke timestamp that WaitTypingIdle consults.
func TestSendRawKeys_UpdatesLastRawInputAt(t *testing.T) {
	m := newTypingTestManager(t)
	f := m.WithFakeTmux()

	// Create a minimal session.
	sess := &Session{
		FullID:      "host-abc",
		ID:          "abc",
		TmuxSession: "cs-abc",
		State:       StateRunning,
	}
	if err := m.store.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}
	_ = f.NewSessionWithSize(sess.TmuxSession, 80, 24)

	before := time.Now()
	if err := m.SendRawKeys(sess.FullID, "h"); err != nil {
		t.Fatalf("SendRawKeys: %v", err)
	}
	after := time.Now()

	m.mu.Lock()
	last := m.lastRawInputAt[sess.FullID]
	m.mu.Unlock()

	if last.Before(before) || last.After(after) {
		t.Errorf("lastRawInputAt not updated by SendRawKeys: got %v, want in [%v, %v]", last, before, after)
	}
}
