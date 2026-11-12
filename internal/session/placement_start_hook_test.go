package session

import (
	"sync"
	"testing"
	"time"
)

// newPlacementHookManager returns a manager whose Start succeeds through the
// subprocess path, which needs no tmux and returns before any pane work. The
// project dir is registered before the manager's Close so the manager shuts
// down first (LIFO), and the background subprocess cannot write into a dir
// that is already being removed.
type placementHookFixture struct {
	mgr        *Manager
	projectDir string
}

func newPlacementHookManager(t *testing.T) placementHookFixture {
	t.Helper()
	dir := t.TempDir()
	projectDir := t.TempDir()
	mgr, err := NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	allowTestTemp(mgr)
	return placementHookFixture{mgr: mgr, projectDir: projectDir}
}

// startPlacementSubprocess starts one subprocess session and waits for it to
// reach a terminal state, so its background runner never outlives the test.
func startPlacementSubprocess(t *testing.T, f placementHookFixture) *Session {
	t.Helper()
	sess, err := f.mgr.Start(t.Context(), "placement hook task", "", f.projectDir,
		&StartOptions{Subprocess: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cur, ok := f.mgr.store.Get(sess.FullID); ok && cur.State.IsTerminal() {
			return sess
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("subprocess session %s did not finish", sess.FullID)
	return sess
}

// The hook runs exactly once per successful Start and receives the new session.
func TestPlacementStartHookRunsOncePerStartWithNewSession(t *testing.T) {
	f := newPlacementHookManager(t)
	mgr := f.mgr

	var mu sync.Mutex
	var got []*Session
	mgr.SetPlacementStartHook(func(s *Session) {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	})

	sess := startPlacementSubprocess(t, f)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("hook calls = %d, want 1", len(got))
	}
	if got[0] != sess {
		t.Errorf("hook got session %v, want the session Start returned %v", got[0], sess)
	}
}

// A failed Start must not invoke the hook.
func TestPlacementStartHookNotRunOnFailedStart(t *testing.T) {
	f := newPlacementHookManager(t)
	mgr := f.mgr
	calls := 0
	mgr.SetPlacementStartHook(func(*Session) { calls++ })

	// An unknown tag is refused before any session is created.
	_, err := mgr.Start(t.Context(), "bad tag", "", f.projectDir,
		&StartOptions{Subprocess: true, Tags: []string{"no-such-tag-xyz"}})
	if err == nil {
		t.Fatal("Start with unknown tag: want error, got nil")
	}
	if calls != 0 {
		t.Errorf("hook calls after failed Start = %d, want 0", calls)
	}
}

// A nil hook is a no-op and Start still succeeds.
func TestPlacementStartHookNilIsNoOp(t *testing.T) {
	f := newPlacementHookManager(t)
	mgr := f.mgr
	sess := startPlacementSubprocess(t, f)
	if sess == nil {
		t.Fatal("Start returned nil session with no hook set")
	}

	mgr.SetPlacementStartHook(nil)
	sess2 := startPlacementSubprocess(t, f)
	if sess2 == nil {
		t.Fatal("Start returned nil session after setting nil hook")
	}
}

// Clearing the hook stops further calls.
func TestPlacementStartHookClearStopsCalls(t *testing.T) {
	f := newPlacementHookManager(t)
	mgr := f.mgr
	calls := 0
	mgr.SetPlacementStartHook(func(*Session) { calls++ })

	startPlacementSubprocess(t, f)
	if calls != 1 {
		t.Fatalf("calls before clear = %d, want 1", calls)
	}

	mgr.SetPlacementStartHook(nil)
	startPlacementSubprocess(t, f)
	if calls != 1 {
		t.Errorf("calls after clear = %d, want still 1", calls)
	}
}

// The hook runs without m.mu held, so it may call back into the Manager.
func TestPlacementStartHookRunsWithoutManagerLock(t *testing.T) {
	f := newPlacementHookManager(t)
	mgr := f.mgr
	done := make(chan struct{})
	mgr.SetPlacementStartHook(func(*Session) {
		// Would deadlock if Start still held m.mu.
		mgr.SetPlacementStartHook(nil)
		close(done)
	})
	startPlacementSubprocess(t, f)
	select {
	case <-done:
	default:
		t.Fatal("hook did not run during Start")
	}
}
