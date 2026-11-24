package session

import (
	"strings"
	"testing"
	"time"
)

func endTestSession(t *testing.T, mgr *Manager, id string, state State) *Session {
	t.Helper()
	s := &Session{ID: id, FullID: "testhost-" + id, TmuxSession: "cs-" + id, State: state, UpdatedAt: time.Now()}
	if err := mgr.SaveSession(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// A killed session lets go of its capacity lease when it is killed, not at the
// next periodic reap.
func TestEndSession_ReleasesLeaseAtKillTime(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	var released []string
	mgr.SetOnSessionLeaseRelease(func(s *Session) { released = append(released, s.FullID) })
	endTestSession(t, mgr, "ab01", StateRunning)
	if err := mgr.EndSession("testhost-ab01", EndReason{Caller: "test", Reason: "operator kill"}); err != nil {
		t.Fatal(err)
	}
	if len(released) != 1 || released[0] != "testhost-ab01" {
		t.Fatalf("lease release hook calls = %v, want one for the killed session", released)
	}
}

// Ending a session that is already finished still frees a lease it may hold.
func TestEndSession_ReleasesLeaseOfAlreadyFinishedSession(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	var n int
	mgr.SetOnSessionLeaseRelease(func(*Session) { n++ })
	endTestSession(t, mgr, "ab02", StateComplete)
	if err := mgr.EndSession("testhost-ab02", EndReason{Caller: "test", Reason: "cleanup"}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("release hook calls = %d, want 1", n)
	}
}

// A kill records the end time and the reason on the session record itself.
func TestEndSession_RecordsEndTimeAndReasonOnRecord(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	endTestSession(t, mgr, "ab03", StateRunning)
	before := time.Now().Add(-time.Second)
	if err := mgr.EndSession("testhost-ab03", EndReason{Caller: "task-respawn", Reason: "task respawned"}); err != nil {
		t.Fatal(err)
	}
	got, _ := mgr.store.Get("testhost-ab03")
	if got.State != StateKilled || got.EndedAt == nil || got.EndedAt.Before(before) {
		t.Fatalf("record not finalised: state=%s ended=%v", got.State, got.EndedAt)
	}
	if !strings.Contains(got.EndNote, "task respawned") || !strings.Contains(got.EndNote, "task-respawn") {
		t.Fatalf("EndNote = %q, want the caller and the reason", got.EndNote)
	}
}

// A repeated end of a finished session keeps the first end time and note.
func TestEndSession_RepeatedEndKeepsFirstRecord(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	endTestSession(t, mgr, "ab04", StateRunning)
	if err := mgr.EndSession("testhost-ab04", EndReason{Caller: "a", Reason: "first"}); err != nil {
		t.Fatal(err)
	}
	first, _ := mgr.store.Get("testhost-ab04")
	at, note := *first.EndedAt, first.EndNote
	time.Sleep(5 * time.Millisecond)
	if err := mgr.EndSession("testhost-ab04", EndReason{Caller: "b", Reason: "second"}); err != nil {
		t.Fatal(err)
	}
	again, _ := mgr.store.Get("testhost-ab04")
	if !again.EndedAt.Equal(at) || again.EndNote != note {
		t.Fatalf("second end rewrote the record: %v %q (was %v %q)", again.EndedAt, again.EndNote, at, note)
	}
}

// Every natural end (complete, failed) funnels through emitSessionEnd, which
// writes the end time and exit note when the record has none.
func TestEmitSessionEnd_StampsEndTimeAndExitNote(t *testing.T) {
	for _, st := range []State{StateComplete, StateFailed} {
		mgr, _ := newTestManagerWithFake(t)
		s := endTestSession(t, mgr, "ac01", st)
		mgr.emitSessionEnd(s)
		got, _ := mgr.store.Get("testhost-ac01")
		if got.EndedAt == nil || !strings.Contains(got.EndNote, string(st)) {
			t.Fatalf("%s: ended=%v note=%q", st, got.EndedAt, got.EndNote)
		}
		keep := *got.EndedAt
		mgr.emitSessionEnd(s)
		again, _ := mgr.store.Get("testhost-ac01")
		if !again.EndedAt.Equal(keep) {
			t.Fatalf("%s: second emit moved the end time", st)
		}
	}
}
