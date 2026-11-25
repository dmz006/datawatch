package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SessionGone must be true only when a tmux server answers. TestMain gives the
// package its own tmux socket directory; this test narrows it further.
func TestTmuxSessionGoneNeedsAnAnswer(t *testing.T) {
	dir, err := os.MkdirTemp("", "g-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	tm := &TmuxManager{}
	if tm.SessionGone("cs-none") {
		t.Fatal("gone with no server to ask")
	}
	if err := TmuxCommand("new-session", "-d", "-s", "cs-here", "sleep 60").Run(); err != nil {
		t.Skipf("tmux not usable here: %v", err)
	}
	t.Cleanup(func() { _ = TmuxCommand("kill-server").Run() })
	if tm.SessionGone("cs-here") {
		t.Fatal("live session reported gone")
	}
	if !tm.SessionGone("cs-none") {
		t.Fatal("missing session not reported gone by a running server")
	}
	if tm.SessionGone("") {
		t.Fatal("empty name reported gone")
	}
}

// silentTmux is a TmuxAPI that cannot say whether a session is gone.
type silentTmux struct{ TmuxAPI }

func TestGuardSessionDeadFailsClosed(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	logs := t.TempDir()
	save := func(id string, s Session) string {
		s.ID, s.FullID, s.UpdatedAt = id, "testhost-"+id, time.Now()
		if s.Hostname == "" {
			s.Hostname = "testhost"
		}
		if err := mgr.SaveSession(&s); err != nil {
			t.Fatal(err)
		}
		return s.FullID
	}
	subprocess := func(id, rc string) string {
		dir := filepath.Join(logs, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if rc != "" {
			if err := os.WriteFile(filepath.Join(dir, subprocessRCFile), []byte(rc), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return save(id, Session{Subprocess: true, State: StateFailed, LogFile: filepath.Join(dir, "o.log")})
	}

	paneless := save("p1", Session{TmuxSession: "cs-p1", State: StateKilled})
	if !mgr.guardSessionDead(paneless) {
		t.Fatal("ended session with no pane not dead")
	}
	for why, id := range map[string]string{
		"no record":                        "testhost-nope",
		"another host":                     save("p2", Session{TmuxSession: "cs-p2", Hostname: "elsewhere", State: StateKilled}),
		"no pane name":                     save("p3", Session{State: StateKilled}),
		"subprocess with no recorded exit": subprocess("s1", ""),
	} {
		if mgr.guardSessionDead(id) {
			t.Errorf("%s: reported dead", why)
		}
	}
	if !mgr.guardSessionDead(subprocess("s2", "0\n")) {
		t.Error("subprocess with a recorded exit not dead")
	}
	mgr.tmux = silentTmux{fake}
	if mgr.guardSessionDead(paneless) {
		t.Error("dead although tmux cannot say")
	}
}

func TestGuardOwnerUnknownWithoutDataDir(t *testing.T) {
	if got := (&Manager{}).guardOwner(); got != "" {
		t.Fatalf("owner %q for a manager with no data directory", got)
	}
}
