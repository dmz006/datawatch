package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests for the repository-scoped guard together with the ownership rules and
// the pane environment. Every test has a private temp directory; the tmux
// tests use the private-server fixture of pane_env_tmux_test.go.

type guardRecordFile struct {
	Owner      string `json:"owner"`
	SessionID  string `json:"session_id"`
	TmuxSocket string `json:"tmux_socket"`
}

func readGuardRecordFile(t *testing.T, dir string) guardRecordFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "datawatch-owner.json"))
	if err != nil {
		t.Fatalf("read the guard's owner record: %v", err)
	}
	var rec guardRecordFile
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("parse the guard's owner record: %v", err)
	}
	return rec
}

func wantProjectRepo(t *testing.T, got, project string) {
	t.Helper()
	want, err := filepath.EvalSymlinks(filepath.Join(project, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("guarded repository = %q, want %q", got, want)
	}
}

// A guard armed by a real launch is scoped to the project, owned by this
// daemon, records the socket of the real private server, and its variables
// reach the worker process there.
func TestPaneLaunchGuardIsScopedOwnedAndDelivered(t *testing.T) {
	stubPaneCeiling(t, false)
	f := newScopeFixture(t)
	envFile := filepath.Join(f.out, "env")
	sess, err := f.m.Start(context.Background(), "do the work", "", f.main, &StartOptions{
		Backend: "pane-env-test", PRDID: "prd1",
		LaunchFn: func(ctx context.Context, _, tmuxSession, dir, _ string) error {
			return f.launchEnvThenGit(ctx, tmuxSession, envFile, dir)
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	f.assertResults()
	g := f.m.commitGuardFor(sess.FullID)
	if g == nil {
		t.Fatal("no guard registered")
	}
	wantProjectRepo(t, g.Repository(), f.main)
	b, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	env := "\n" + string(b)
	for _, want := range append(g.Env(), "DATAWATCH_SESSION_ID="+sess.FullID,
		PaneSessionMarkerEnv+"="+PaneSessionMarker(sess.FullID), "DATAWATCH_BASE_URL=http://127.0.0.1:18443") {
		if !strings.Contains(env, "\n"+want+"\n") {
			t.Errorf("the worker did not receive %s", want)
		}
	}
	if strings.Contains(env, g.Token()) {
		t.Error("the worker received the commit token")
	}
	socket, err := f.tmux("display-message", "-p", "#{socket_path}")
	if err != nil {
		t.Fatalf("socket path: %v: %s", err, socket)
	}
	want := guardRecordFile{Owner: f.m.guardOwner(), SessionID: sess.FullID, TmuxSocket: strings.TrimSpace(socket)}
	if got := readGuardRecordFile(t, g.Dir()); got != want {
		t.Errorf("owner record = %+v, want %+v", got, want)
	}
}

func (f *scopeFixture) launchEnvThenGit(ctx context.Context, tmuxSession, envFile, project string) error {
	return f.m.tmux.SendKeys(tmuxSession, "env > "+shellSingleQuote(envFile)+"; "+f.command(project, f.linked))
}

// Reaping a scoped guard on a real private tmux server: it stays while the
// pane lives, when another daemon starts, and when the daemon reaches another
// server; it goes once its own server says the session is gone.
func TestScopedGuardReapOnRealTmux(t *testing.T) {
	f := newScopeFixture(t)
	const pane = "cs-reap"
	if out, err := f.tmux("new-session", "-d", "-s", pane, "/bin/sh"); err != nil {
		t.Fatalf("new session: %v: %s", err, out)
	}
	sess := &Session{ID: "reap", FullID: "testhost-reap", TmuxSession: pane, Hostname: "testhost",
		State: StateKilled, PRDID: "p", ProjectDir: f.main, UpdatedAt: time.Now()}
	if err := f.m.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	f.m.armCommitGuard(sess, pane)
	g := f.m.commitGuardFor(sess.FullID)
	if g == nil {
		t.Fatal("no guard armed")
	}
	wantProjectRepo(t, g.Repository(), f.main)
	if got := readGuardRecordFile(t, g.Dir()).TmuxSocket; got == "" || got != f.m.tmuxSocket() {
		t.Fatalf("recorded socket %q, daemon's %q", got, f.m.tmuxSocket())
	}

	f.m.adoptCommitGuards(nil)
	reapOwnerWantDir(t, g.Dir(), "pane still alive")

	other, _ := reapOwnerManager(t)
	other.tmux = f.m.tmux
	copied := *sess
	if err := other.SaveSession(&copied); err != nil {
		t.Fatal(err)
	}
	if out, err := f.tmux("kill-session", "-t", pane); err != nil {
		t.Fatalf("kill session: %v: %s", err, out)
	}
	other.adoptCommitGuards(nil)
	reapOwnerWantDir(t, g.Dir(), "another daemon with a dead record of the same id")

	// Point the daemon's tmux at a second private server, which truthfully
	// does not have the session.
	wrapper, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	if wrapper == f.realBin {
		t.Fatal("setup: the private tmux wrapper is not first on PATH")
	}
	own, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	second := f.sock + "2"
	script := "#!/bin/sh\nunset TMUX TMUX_TMPDIR\nexec " + shellSingleQuote(f.realBin) + " -S " + shellSingleQuote(second) + " -f /dev/null \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil { // #nosec G306 -- test wrapper
		t.Fatal(err)
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "holder", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("second server: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command(f.realBin, "-S", second, "-f", os.DevNull, "kill-server").Run()
	})
	if !f.m.guardSessionDead(sess.FullID) {
		t.Fatal("setup: the second server should answer that the session is gone")
	}
	f.m.adoptCommitGuards(nil)
	reapOwnerWantDir(t, g.Dir(), "another tmux server answered")

	if err := os.WriteFile(wrapper, own, 0o700); err != nil { // #nosec G306 -- test wrapper
		t.Fatal(err)
	}
	f.m.adoptCommitGuards(nil)
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("own dead guard on its own server kept: %v", err)
	}
}

// A subprocess guard is scoped and owned, records no tmux socket, and is
// reaped only once the subprocess has a recorded exit.
func TestSubprocessScopedGuardIsOwnedAndReapedOnlyAfterItsExit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	project := guardTestRepo(t)
	mgr, _ := reapOwnerManager(t)
	logs := t.TempDir()
	sess := &Session{ID: "subp", FullID: "testhost-subp", Hostname: "testhost", Subprocess: true,
		State: StateFailed, PRDID: "p", ProjectDir: project, LogFile: filepath.Join(logs, "o.log"), UpdatedAt: time.Now()}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	mgr.prepareSubprocessEnv(sess, nil)
	g := mgr.commitGuardFor(sess.FullID)
	if g == nil {
		t.Fatal("no guard armed")
	}
	wantProjectRepo(t, g.Repository(), project)
	want := guardRecordFile{Owner: mgr.guardOwner(), SessionID: sess.FullID}
	if got := readGuardRecordFile(t, g.Dir()); got != want {
		t.Fatalf("owner record = %+v, want %+v", got, want)
	}
	if env := strings.Join(mgr.takeSubprocessEnv(sess.FullID), "\n"); !strings.Contains(env, "GIT_CONFIG_VALUE_0="+g.Dir()) {
		t.Fatalf("subprocess environment lacks the guard: %s", env)
	}
	mgr.adoptCommitGuards(nil)
	reapOwnerWantDir(t, g.Dir(), "no recorded exit")
	if err := os.WriteFile(filepath.Join(logs, subprocessRCFile), []byte("0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr.adoptCommitGuards(nil)
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("own dead subprocess guard kept: %v", err)
	}
}
