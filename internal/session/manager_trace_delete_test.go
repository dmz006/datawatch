// Deleting a session drops its trace — and only its trace. The
// trace-deleter hook is wired like the other observe hooks: Manager.Delete
// calls it after the session record is gone, matched on the exact FullID,
// best-effort (a trace failure never fails the delete). Ending, killing
// and archiving a session never touch the trace; only an explicit delete
// (including the Automaton cleanup callback path, which goes through
// Manager.Delete with deleteData=false) does.

package session

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeTraceDeleter records the exact session ids DeleteSession was called
// with and optionally fails, standing in for *trace.Store.
type fakeTraceDeleter struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeTraceDeleter) DeleteSession(sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sessionID)
	return f.err
}

func (f *fakeTraceDeleter) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func newTraceDeleteTestMgr(t *testing.T, dataDir string) *Manager {
	t.Helper()
	mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	allowTestTemp(mgr)
	mgr.WithFakeTmux()
	return mgr
}

func saveTerminalSession(t *testing.T, mgr *Manager, id, fullID string) {
	t.Helper()
	now := time.Now()
	if err := mgr.SaveSession(&Session{
		ID: id, FullID: fullID, TmuxSession: "cs-" + id,
		ProjectDir: "",
		State:      StateComplete,
		CreatedAt:  now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveSession(%s): %v", fullID, err)
	}
}

// TestDelete_RemovesTraceOfExactIDOnly: deleting one session calls the
// deleter exactly once with that session's FullID; a session whose id only
// shares a prefix keeps its trace (the deleter is never called for it).
func TestDelete_RemovesTraceOfExactIDOnly(t *testing.T) {
	mgr := newTraceDeleteTestMgr(t, t.TempDir())

	saveTerminalSession(t, mgr, "ab01", "testhost-ab01")
	saveTerminalSession(t, mgr, "ab013f", "testhost-ab013f") // shares the prefix "testhost-ab01"

	deleter := &fakeTraceDeleter{}
	mgr.SetTraceDeleter(deleter)

	if err := mgr.Delete("testhost-ab01", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	calls := deleter.snapshot()
	if len(calls) != 1 || calls[0] != "testhost-ab01" {
		t.Fatalf("deleter calls = %v, want exactly [testhost-ab01]", calls)
	}
	if _, ok := mgr.GetSession("testhost-ab013f"); !ok {
		t.Fatalf("session testhost-ab013f (shared prefix) was removed too")
	}
}

// TestEndedAndArchivedSessions_KeepTrace: ending (killing) and archiving a
// session never calls the deleter — the trace survives both transitions.
func TestEndedAndArchivedSessions_KeepTrace(t *testing.T) {
	mgr := newTraceDeleteTestMgr(t, t.TempDir())
	saveTerminalSession(t, mgr, "ke01", "testhost-ke01")

	deleter := &fakeTraceDeleter{}
	mgr.SetTraceDeleter(deleter)

	// End it (already terminal, but this is the end path a running session
	// would take).
	if err := mgr.EndSession("testhost-ke01", EndReason{Caller: "test", Reason: "cleanup"}); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if calls := deleter.snapshot(); len(calls) != 0 {
		t.Fatalf("ending a session must not drop its trace, deleter calls = %v", calls)
	}

	// Archive it (age past the archive window, then run one pass with a
	// fixed now).
	mgr.SetArchiveAfter(time.Nanosecond)
	if moved, err := mgr.ArchiveSessions(time.Now()); err != nil {
		t.Fatalf("ArchiveSessions: %v", err)
	} else if moved != 1 {
		t.Fatalf("archived %d sessions, want 1", moved)
	}
	if calls := deleter.snapshot(); len(calls) != 0 {
		t.Fatalf("archiving a session must not drop its trace, deleter calls = %v", calls)
	}
	arch := mgr.ListArchivedSessions(SessionFilter{}, nil, "", time.Now().Add(time.Hour), 10)
	if len(arch) == 0 || arch[0].FullID != "testhost-ke01" {
		t.Fatalf("session testhost-ke01 not found in archive, got %d entries", len(arch))
	}
}

// TestDelete_TraceDeleterErrorDoesNotFailDelete: a failing deleter is
// logged, not propagated — the session is still removed.
func TestDelete_TraceDeleterErrorDoesNotFailDelete(t *testing.T) {
	mgr := newTraceDeleteTestMgr(t, t.TempDir())
	saveTerminalSession(t, mgr, "df01", "testhost-df01")

	mgr.SetTraceDeleter(&fakeTraceDeleter{err: errors.New("trace store on fire")})

	if err := mgr.Delete("testhost-df01", false); err != nil {
		t.Fatalf("Delete must not fail on a trace-deleter error: %v", err)
	}
	if _, ok := mgr.GetSession("testhost-df01"); ok {
		t.Fatalf("session testhost-df01 still present despite successful Delete")
	}
}

// TestDelete_viaAutomatonCleanup_RemovesTrace: the Automaton cleanup
// callback (manager.go SetSessionDeleteFn -> mgr.Delete(fullID, false))
// goes through Manager.Delete, so the same delete path that deletes a
// session's record also drops its trace.
func TestDelete_viaAutomatonCleanup_RemovesTrace(t *testing.T) {
	mgr := newTraceDeleteTestMgr(t, t.TempDir())
	saveTerminalSession(t, mgr, "ax01", "testhost-ax01")

	deleter := &fakeTraceDeleter{}
	mgr.SetTraceDeleter(deleter)

	// The exact shape of the wired callback in cmd/datawatch/main.go.
	cleanup := func(sessionID string) {
		sess, ok := mgr.GetSession(sessionID)
		if !ok {
			return
		}
		_ = mgr.Delete(sess.FullID, false)
	}
	cleanup("testhost-ax01")

	calls := deleter.snapshot()
	if len(calls) != 1 || calls[0] != "testhost-ax01" {
		t.Fatalf("deleter calls = %v, want exactly [testhost-ax01]", calls)
	}
}
