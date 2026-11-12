// Tests for session end attribution (end_reason.go): the timeline and journal
// record who ended a session and why, and a terminal state stays sticky.

package session

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// newEndTestSession saves a session in the given state with a timeline tracker
// attached, so an end reaches timeline.md the same way a live session's does.
func newEndTestSession(t *testing.T, mgr *Manager, suffix string, state State) (*Session, *Tracker) {
	t.Helper()
	now := time.Now()
	sess := &Session{
		ID:          suffix,
		FullID:      "testhost-" + suffix,
		TmuxSession: "cs-" + suffix,
		Task:        "end attribution " + suffix,
		State:       state,
		CreatedAt:   now,
		UpdatedAt:   now,
		Hostname:    "testhost",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}
	tracker, err := NewTracker(mgr.dataDir, sess)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	mgr.trackers[sess.FullID] = tracker
	return sess, tracker
}

// endTimelineLines returns the timeline lines written when a session ends.
func endTimelineLines(t *testing.T, tracker *Tracker) []string {
	t.Helper()
	lines, err := tracker.ReadTimeline()
	if err != nil {
		t.Fatalf("ReadTimeline: %v", err)
	}
	var out []string
	for _, line := range lines {
		if strings.Contains(line, "session ended") {
			out = append(out, line)
		}
	}
	return out
}

func TestSessionEnd_TimelineCarriesCallerAndReason(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess, tracker := newEndTestSession(t, mgr, "tl01", StateRunning)

	if err := mgr.EndSession(sess.FullID, EndReason{Caller: "test-operator", Reason: "stop requested by operator"}); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	lines := endTimelineLines(t, tracker)
	if len(lines) != 1 {
		t.Fatalf("want one session-ended line, got %d: %q", len(lines), lines)
	}
	want := "| killed | session ended | by=test-operator | reason=stop requested by operator"
	if !strings.Contains(lines[0], want) {
		t.Errorf("timeline line %q does not contain %q", lines[0], want)
	}
}

func TestSessionEnd_LegacyKillDerivesCaller(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess, tracker := newEndTestSession(t, mgr, "lk01", StateRunning)

	if err := mgr.Kill(sess.FullID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	lines := endTimelineLines(t, tracker)
	if len(lines) != 1 {
		t.Fatalf("want one session-ended line, got %d: %q", len(lines), lines)
	}
	// The caller is the test function that called Kill, with its file and line.
	wantBy := "by=session.TestSessionEnd_LegacyKillDerivesCaller@end_reason_test.go:"
	if !strings.Contains(lines[0], wantBy) {
		t.Errorf("timeline line %q does not attribute the legacy Kill to its caller (want %q)", lines[0], wantBy)
	}
	if !strings.Contains(lines[0], "reason="+noReasonGiven) {
		t.Errorf("timeline line %q does not carry the no-reason text %q", lines[0], noReasonGiven)
	}
}

func TestSessionEnd_CascadeChildCarriesParentReason(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	parent, _ := newEndTestSession(t, mgr, "cp01", StateRunning)
	parent.KillChildren = true
	if err := mgr.store.Save(parent); err != nil {
		t.Fatalf("save parent: %v", err)
	}
	child, childTracker := newEndTestSession(t, mgr, "cc01", StateRunning)
	child.ParentID = parent.FullID
	if err := mgr.store.Save(child); err != nil {
		t.Fatalf("save child: %v", err)
	}

	if err := mgr.EndSession(parent.FullID, EndReason{Caller: "cascade-test", Reason: "stop requested by test"}); err != nil {
		t.Fatalf("EndSession parent: %v", err)
	}

	got, _ := mgr.store.Get(child.FullID)
	if got.State != StateKilled {
		t.Fatalf("child state = %s, want %s", got.State, StateKilled)
	}
	lines := endTimelineLines(t, childTracker)
	if len(lines) != 1 {
		t.Fatalf("want one session-ended line for the child, got %d: %q", len(lines), lines)
	}
	want := "| by=cascade-test | reason=parent session " + parent.ID + " ended"
	if !strings.Contains(lines[0], want) {
		t.Errorf("child timeline line %q does not carry the parent's caller and reason (want %q)", lines[0], want)
	}
}

func TestSessionEnd_ChannelEventCompleteRecordsCaller(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess, tracker := newEndTestSession(t, mgr, "ce01", StateRunning)

	mgr.MarkChannelEvent(sess.FullID, EventComplete)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateComplete {
		t.Fatalf("state after complete event = %s, want %s", got.State, StateComplete)
	}
	lines := endTimelineLines(t, tracker)
	if len(lines) != 1 {
		t.Fatalf("want one session-ended line, got %d: %q", len(lines), lines)
	}
	want := "| by=channel-event | reason=channel event: complete"
	if !strings.Contains(lines[0], want) {
		t.Errorf("timeline line %q does not attribute the complete to the channel event (want %q)", lines[0], want)
	}
}

func TestSessionEnd_TerminalStateStaysSticky(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess, tracker := newEndTestSession(t, mgr, "st01", StateRunning)

	if err := mgr.EndSession(sess.FullID, EndReason{Caller: "first-end", Reason: "first"}); err != nil {
		t.Fatalf("first EndSession: %v", err)
	}
	if err := mgr.EndSession(sess.FullID, EndReason{Caller: "second-end", Reason: "second"}); err != nil {
		t.Fatalf("second EndSession: %v", err)
	}

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateKilled {
		t.Errorf("state after two ends = %s, want %s", got.State, StateKilled)
	}
	lines := endTimelineLines(t, tracker)
	if len(lines) != 1 {
		t.Errorf("second end duplicated the state change: got %d session-ended lines: %q", len(lines), lines)
	}
	if len(lines) == 1 && !strings.Contains(lines[0], "by=first-end") {
		t.Errorf("timeline should keep the first end's attribution, got %q", lines[0])
	}

	// A complete session stays complete when it is ended later.
	done, _ := newEndTestSession(t, mgr, "st02", StateComplete)
	if err := mgr.EndSession(done.FullID, EndReason{Caller: "late-end", Reason: "late"}); err != nil {
		t.Fatalf("EndSession on complete session: %v", err)
	}
	got, _ = mgr.store.Get(done.FullID)
	if got.State != StateComplete {
		t.Errorf("complete session became %s after a later end, want %s", got.State, StateComplete)
	}
}

func TestSessionEnd_JournalCapturesEnd(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	var journal bytes.Buffer
	mgr.journal = &journal
	sess, _ := newEndTestSession(t, mgr, "jr01", StateRunning)

	if err := mgr.EndSession(sess.FullID, EndReason{Caller: "journal-test", Reason: "capture check"}); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	out := journal.String()
	wantEnd := fmt.Sprintf("[session] ended jr01: %s -> %s by=journal-test reason=capture check", StateRunning, StateKilled)
	if !strings.Contains(out, wantEnd) {
		t.Errorf("journal %q does not contain %q", out, wantEnd)
	}
	wantPane := "[session] closing pane cs-jr01 for jr01: capture check"
	if !strings.Contains(out, wantPane) {
		t.Errorf("journal %q does not contain %q", out, wantPane)
	}
	if fake.Count("kill") != 1 {
		t.Errorf("want one pane kill, got calls: %+v", fake.Calls)
	}
}
