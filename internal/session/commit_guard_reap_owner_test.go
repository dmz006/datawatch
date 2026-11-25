package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// Every test here uses its own private temp directory: reaping walks the temp
// directory, so it must never see a real guard folder.

// reapOwnerManager returns a manager with its own data directory.
func reapOwnerManager(t *testing.T) (*Manager, *FakeTmux) {
	t.Helper()
	return newTestManagerWithFake(t)
}

// reapOwnerSession saves a session record on mgr and arms its guard.
func reapOwnerSession(t *testing.T, mgr *Manager, id string, state State) (*Session, *gitguard.CommitGuard) {
	t.Helper()
	sess := &Session{
		ID: id, FullID: "testhost-" + id, TmuxSession: "cs-" + id,
		Hostname: "testhost", State: state, PRDID: "p", UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	mgr.armCommitGuard(sess, "")
	g := mgr.commitGuardFor(sess.FullID)
	if g == nil {
		t.Fatal("no guard armed")
	}
	return sess, g
}

func reapOwnerWantDir(t *testing.T, dir, why string) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("%s: guard folder removed: %v", why, err)
	}
}

func reapOwnerResume(t *testing.T, mgr *Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mgr.ResumeMonitors(ctx)
}

func TestGuardReapKeepsAnotherDaemonsGuard(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	running, _ := reapOwnerManager(t)
	_, g := reapOwnerSession(t, running, "aaaa", StateRunning)
	other, _ := reapOwnerManager(t)
	reapOwnerResume(t, other)
	reapOwnerWantDir(t, g.Dir(), "another daemon started")
}

// The other daemon also has a dead record under the same session ID (a copied
// data directory, or a test fixture that reuses an ID).
func TestGuardReapKeepsAnotherDaemonsGuardWithSameSessionID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	running, _ := reapOwnerManager(t)
	_, g := reapOwnerSession(t, running, "aaaa", StateRunning)
	other, _ := reapOwnerManager(t)
	_ = other.SaveSession(&Session{
		ID: "aaaa", FullID: "testhost-aaaa", TmuxSession: "cs-aaaa",
		Hostname: "testhost", State: StateKilled, PRDID: "p", UpdatedAt: time.Now(),
	})
	reapOwnerResume(t, other)
	reapOwnerWantDir(t, g.Dir(), "same session ID, different owner")
}

func TestGuardReapKeepsUnknownOwnerGuard(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	g, err := gitguard.NewCommitGuard("testhost-bbbb")
	if err != nil {
		t.Fatal(err)
	}
	mgr, _ := reapOwnerManager(t)
	_ = mgr.SaveSession(&Session{
		ID: "bbbb", FullID: "testhost-bbbb", TmuxSession: "cs-bbbb",
		Hostname: "testhost", State: StateKilled, PRDID: "p", UpdatedAt: time.Now(),
	})
	reapOwnerResume(t, mgr)
	reapOwnerWantDir(t, g.Dir(), "no owner recorded")
}

func TestGuardReapKeepsOwnGuardWithoutSessionRecord(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	mgr, _ := reapOwnerManager(t)
	sess := &Session{ID: "cccc", FullID: "testhost-cccc", Hostname: "testhost", PRDID: "p"}
	mgr.armCommitGuard(sess, "") // never saved: the daemon cannot tell
	g := mgr.commitGuardFor(sess.FullID)
	if g == nil {
		t.Fatal("no guard armed")
	}
	mgr.adoptCommitGuards(nil)
	reapOwnerWantDir(t, g.Dir(), "no session record")
}

func TestGuardReapKeepsOwnGuardWhilePaneAlive(t *testing.T) {
	for _, state := range []State{StateFailed, StateComplete, StateKilled, StateRunning} {
		t.Run(string(state), func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			mgr, fake := reapOwnerManager(t)
			sess, g := reapOwnerSession(t, mgr, "dddd", state)
			if err := fake.NewSessionWithSize(sess.TmuxSession, 80, 24); err != nil {
				t.Fatal(err)
			}
			mgr.adoptCommitGuards(nil) // not on the live list, but the pane is there
			reapOwnerWantDir(t, g.Dir(), "pane still alive")
		})
	}
}

func TestGuardReapRemovesOwnDeadGuardAfterRestart(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dataDir := t.TempDir()
	start := func() *Manager {
		mgr, err := NewManager("testhost", dataDir, "/bin/echo", 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = mgr.Close() })
		allowTestTemp(mgr)
		mgr.WithFakeTmux()
		return mgr
	}
	first := start()
	_, g := reapOwnerSession(t, first, "eeee", StateKilled)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reapOwnerResume(t, start())
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("own dead guard kept: %v", err)
	}
}

// Adopting must not take another daemon's folder: releasing the session would
// then delete it.
func TestGuardAdoptAndReleaseLeaveAnotherDaemonsGuard(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	running, _ := reapOwnerManager(t)
	_, g := reapOwnerSession(t, running, "ffff", StateRunning)
	other, _ := reapOwnerManager(t)
	sess := &Session{ID: "ffff", FullID: "testhost-ffff", TmuxSession: "cs-ffff", Hostname: "testhost", PRDID: "p"}
	other.adoptCommitGuards([]*Session{sess})
	if got := other.commitGuardFor(sess.FullID); got == nil || got.Dir() == g.Dir() {
		t.Fatalf("expected a guard of its own, got %+v", got)
	}
	other.releaseCommitGuard(sess.FullID, "")
	reapOwnerWantDir(t, g.Dir(), "released by another daemon")
}

// A folder made before owners were recorded is still adopted, so the running
// worker stays guarded, but it is never deleted.
func TestGuardAdoptedUnknownOwnerGuardIsNotDeleted(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	old, err := gitguard.NewCommitGuard("testhost-abab")
	if err != nil {
		t.Fatal(err)
	}
	mgr, _ := reapOwnerManager(t)
	sess := &Session{ID: "abab", FullID: "testhost-abab", TmuxSession: "cs-abab", Hostname: "testhost", PRDID: "p"}
	mgr.adoptCommitGuards([]*Session{sess})
	got := mgr.commitGuardFor(sess.FullID)
	if got == nil || got.Dir() != old.Dir() || got.Token() != old.Token() {
		t.Fatalf("old guard not adopted: %+v", got)
	}
	mgr.releaseCommitGuard(sess.FullID, "")
	reapOwnerWantDir(t, old.Dir(), "ownership unknown")
}
