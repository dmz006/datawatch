package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// Every test here uses its own private temp directory and, for real tmux, its
// own private socket directories.

// guardTmuxServer makes a private socket directory, points tmux at it and
// starts a server there holding one session of the given name. The directory
// must exist first: tmux falls back to the default socket when TMUX_TMPDIR
// does not. Call it before narrowing TMPDIR, to keep the socket path short.
func guardTmuxServer(t *testing.T, session string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "g-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	if err := TmuxCommand("new-session", "-d", "-s", session, "sleep 60").Run(); err != nil {
		t.Skipf("tmux not usable here: %v", err)
	}
	sock := filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), "default")
	t.Cleanup(func() { _ = TmuxCommand("-S", sock, "kill-server").Run() })
	return dir
}

// guardOnRealTmux arms a guard for a saved session that is live on the tmux
// server TMUX_TMPDIR points at.
func guardOnRealTmux(t *testing.T, mgr *Manager, id string) (*Session, *gitguard.CommitGuard) {
	t.Helper()
	mgr.tmux = &TmuxManager{}
	sess := &Session{
		ID: id, FullID: "testhost-" + id, TmuxSession: "cs-" + id,
		Hostname: "testhost", State: StateRunning, PRDID: "p", UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	mgr.armCommitGuard(sess, sess.TmuxSession)
	g := mgr.commitGuardFor(sess.FullID)
	if g == nil {
		t.Fatal("no guard armed")
	}
	return sess, g
}

// The daemon comes back with another TMUX_TMPDIR whose server is running: its
// "can't find session" says nothing about the server the session is on.
func TestGuardReapKeepsGuardWhenAnotherTmuxServerAnswers(t *testing.T) {
	other := guardTmuxServer(t, "cs-unrelated")
	own := guardTmuxServer(t, "cs-aaaa")
	t.Setenv("TMPDIR", t.TempDir())
	mgr, _ := reapOwnerManager(t)
	_, g := guardOnRealTmux(t, mgr, "aaaa")

	t.Setenv("TMUX_TMPDIR", other)
	reapOwnerResume(t, mgr)
	reapOwnerWantDir(t, g.Dir(), "asked a different tmux server")
	t.Setenv("TMUX_TMPDIR", own)
	if !(&TmuxManager{}).SessionExists("cs-aaaa") {
		t.Fatal("test setup: session should still be live on its own server")
	}
}

func TestGuardReapRemovesOwnDeadGuardOnItsOwnTmuxServer(t *testing.T) {
	guardTmuxServer(t, "cs-keeper")
	if err := TmuxCommand("new-session", "-d", "-s", "cs-bbbb", "sleep 60").Run(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	mgr, _ := reapOwnerManager(t)
	sess, g := guardOnRealTmux(t, mgr, "bbbb")
	if err := mgr.tmux.KillSession(sess.TmuxSession); err != nil {
		t.Fatal(err)
	}
	mgr.adoptCommitGuards(nil)
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("own dead guard kept: %v", err)
	}
}

// The recorded socket is the one the session was created on. A later adoption
// through another server (one with a session of the same name) must not
// rewrite it.
func TestGuardAdoptDoesNotMoveRecordedTmuxSocket(t *testing.T) {
	other := guardTmuxServer(t, "cs-cccc")
	if err := TmuxCommand("new-session", "-d", "-s", "cs-keeper", "sleep 60").Run(); err != nil {
		t.Fatal(err)
	}
	guardTmuxServer(t, "cs-cccc")
	t.Setenv("TMPDIR", t.TempDir())
	mgr, _ := reapOwnerManager(t)
	sess, g := guardOnRealTmux(t, mgr, "cccc")

	t.Setenv("TMUX_TMPDIR", other)
	mgr.commitGuards = nil // as after a restart
	mgr.adoptCommitGuards([]*Session{sess})
	if err := mgr.tmux.KillSession(sess.TmuxSession); err != nil {
		t.Fatal(err)
	}
	mgr.adoptCommitGuards(nil)
	reapOwnerWantDir(t, g.Dir(), "namesake ended on a different tmux server")
}

// A guard whose record names no tmux socket cannot be proven dead.
func TestGuardReapKeepsGuardWithUnknownTmuxSocket(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	mgr, _ := reapOwnerManager(t)
	g, err := gitguard.NewOwnedCommitGuard(mgr.guardOwner(), "testhost-dddd")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveSession(&Session{
		ID: "dddd", FullID: "testhost-dddd", TmuxSession: "cs-dddd",
		Hostname: "testhost", State: StateKilled, PRDID: "p", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	mgr.adoptCommitGuards(nil)
	reapOwnerWantDir(t, g.Dir(), "socket unknown")
}

// A restarted daemon that cannot set the tmux environment again must not
// delete the guard the live worker still points at, and must keep its token.
func TestGuardAdoptSurvivesFailedSetEnvironment(t *testing.T) {
	for _, recorded := range []bool{true, false} {
		t.Run(fmt.Sprintf("recorded=%v", recorded), func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			mgr, fake := reapOwnerManager(t)
			owner := ""
			if recorded {
				owner = mgr.guardOwner()
			}
			g, err := gitguard.NewOwnedCommitGuard(owner, "testhost-eeee")
			if err != nil {
				t.Fatal(err)
			}
			sess := &Session{ID: "eeee", FullID: "testhost-eeee", TmuxSession: "cs-eeee", Hostname: "testhost", PRDID: "p"}
			fake.FailNextOp = map[string]int{"set-env": 1}
			mgr.adoptCommitGuards([]*Session{sess})
			reapOwnerWantDir(t, g.Dir(), "SetEnvironment failed on adoption")
			got := mgr.commitGuardFor(sess.FullID)
			if got == nil || got.Dir() != g.Dir() || got.Token() != g.Token() {
				t.Fatalf("guard not kept in the manager: %+v", got)
			}
		})
	}
}

// A guard made for this call was never handed to a worker: a failure to
// install it still removes it.
func TestGuardArmRemovesNewGuardWhenSetEnvironmentFails(t *testing.T) {
	for _, path := range []string{"arm", "adopt with nothing to adopt"} {
		t.Run(path, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			mgr, fake := reapOwnerManager(t)
			sess := &Session{ID: "ffff", FullID: "testhost-ffff", TmuxSession: "cs-ffff", Hostname: "testhost", PRDID: "p"}
			fake.FailNextOp = map[string]int{"set-env": 1}
			if path == "arm" {
				mgr.armCommitGuard(sess, sess.TmuxSession)
			} else {
				mgr.adoptCommitGuards([]*Session{sess})
			}
			if mgr.commitGuardFor(sess.FullID) != nil {
				t.Fatal("failed guard registered")
			}
			left, err := filepath.Glob(filepath.Join(tmp, "datawatch-commitguard-*"))
			if err != nil || len(left) != 0 {
				t.Fatalf("new guard folder left behind: %v %v", left, err)
			}
		})
	}
}

// unnamedTmux can say a session is gone but not which server said so.
type unnamedTmux struct{ *FakeTmux }

func (unnamedTmux) SocketPath() string { return "" }

func TestGuardReapKeepsGuardWhenCurrentTmuxSocketUnknownOrDifferent(t *testing.T) {
	for _, why := range []string{"unknown", "different"} {
		t.Run(why, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			mgr, fake := reapOwnerManager(t)
			_, g := reapOwnerSession(t, mgr, "abcd", StateKilled)
			if why == "unknown" {
				mgr.tmux = unnamedTmux{fake}
			} else {
				fake.Socket = "/elsewhere/default"
			}
			mgr.adoptCommitGuards(nil)
			reapOwnerWantDir(t, g.Dir(), why+" current socket")
		})
	}
}

// The real tmux wrapper names the server it reaches, and nothing without one.
func TestTmuxSocketPath(t *testing.T) {
	empty, err := os.MkdirTemp("", "g-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(empty) })
	tm := &TmuxManager{}
	a := guardTmuxServer(t, "cs-a")
	pathA := tm.SocketPath()
	guardTmuxServer(t, "cs-b")
	pathB := tm.SocketPath()
	if pathA == "" || pathB == "" || pathA == pathB {
		t.Fatalf("socket paths %q and %q", pathA, pathB)
	}
	t.Setenv("TMUX_TMPDIR", a)
	if got := tm.SocketPath(); got != pathA {
		t.Fatalf("socket %q, want %q again", got, pathA)
	}
	t.Setenv("TMUX_TMPDIR", empty)
	if got := tm.SocketPath(); got != "" {
		t.Fatalf("socket %q with no server", got)
	}
}

// Adopting a folder the manager already holds replaces the object, not the
// folder: cleaning up the old one would delete the guard in use.
func TestGuardAdoptTwiceKeepsTheFolder(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("setenv fails=%v", fail), func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			mgr, fake := reapOwnerManager(t)
			sess, g := reapOwnerSession(t, mgr, "gggg", StateRunning)
			if fail {
				fake.FailNextOp = map[string]int{"set-env": 1}
			}
			mgr.adoptCommitGuards([]*Session{sess})
			reapOwnerWantDir(t, g.Dir(), "adopted again")
			if got := mgr.commitGuardFor(sess.FullID); got == nil || got.Dir() != g.Dir() {
				t.Fatalf("guard lost: %+v", got)
			}
		})
	}
}
