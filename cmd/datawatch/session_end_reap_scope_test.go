package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// startScopeFixture starts a harmless sleeping script in its own process
// session. dir is its working directory; script is the file it runs; extraEnv
// is added to its environment (the real environment minus any session id).
func startScopeFixture(t *testing.T, dir, script string, extraEnv ...string) int {
	t.Helper()
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	return pid
}

func scopeReaper() *autonomouspkg.OrphanReaper {
	return &autonomouspkg.OrphanReaper{Grace: time.Second, KillSettle: time.Second}
}

// A process that is not the session's own, but runs a script stored in the
// session's project folder (a watcher the operator parked there), must survive
// the session's end. A project folder is shared; only the session's own tree is
// ended.
func TestSessionEndReap_OutsideProcessWithProjectDirInArgvSurvives(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	pid := startScopeFixture(t, proj, filepath.Join(proj, "watch.sh"))
	called := false
	res := reapSessionEnd(&session.Session{FullID: "host-ab12cd34", ProjectDir: proj}, scopeReaper(),
		func(string, int, []string) bool { called = true; return true },
		func(auditpkg.Entry) { called = true })
	if processGone(pid) {
		t.Fatalf("a process outside the session was ended by the session's end: %+v", res)
	}
	if res.Count != 0 || called {
		t.Fatalf("nothing is the session's own, yet reap reported %+v called=%v", res, called)
	}
}

// A process carrying the session's id in its environment is the session's own
// and is ended, whatever folder it runs in.
func TestSessionEndReap_OwnProcessByEnvironmentIsEnded(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	proj := t.TempDir()
	const id = "host-ab12cd34"
	pid := startScopeFixture(t, elsewhere, filepath.Join(elsewhere, "own.sh"), "DATAWATCH_SESSION_ID="+id)
	res := reapSessionEnd(&session.Session{FullID: id, ProjectDir: proj}, scopeReaper(), nil, nil)
	if res.Count < 1 || !waitGone(pid, time.Now().Add(2*time.Second)) {
		t.Fatalf("the session's own process was not ended: %+v", res)
	}
}

// A process of a different session in the same project folder survives.
func TestSessionEndReap_OtherSessionInSameFolderSurvives(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	pid := startScopeFixture(t, proj, filepath.Join(proj, "other.sh"), "DATAWATCH_SESSION_ID=host-99998888")
	res := reapSessionEnd(&session.Session{FullID: "host-ab12cd34", ProjectDir: proj}, scopeReaper(), nil, nil)
	if processGone(pid) || res.Count != 0 {
		t.Fatalf("another session's process was ended: %+v", res)
	}
}

// A linked git worktree is made for one session, so it is the session's own
// folder: a leftover process running a script there is ended.
func TestSessionEndReap_OwnedWorktreeFolderProcessIsEnded(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	// A linked worktree has a .git FILE (gitdir pointer), not a directory.
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /nonexistent/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid := startScopeFixture(t, wt, filepath.Join(wt, "left.sh"))
	res := reapSessionEnd(&session.Session{FullID: "host-ab12cd34", ProjectDir: wt}, scopeReaper(), nil, nil)
	if res.Count < 1 || !waitGone(pid, time.Now().Add(2*time.Second)) {
		t.Fatalf("a leftover process in the session's own worktree was not ended: %+v", res)
	}
}

// The shared folder rule: a folder that holds a git repository (a .git
// directory) is a project, never a session's own folder.
func TestSessionOwnedDirs(t *testing.T) {
	plain := t.TempDir()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	track := t.TempDir()
	cases := []struct {
		name string
		s    session.Session
		want int
	}{
		{"plain shared folder", session.Session{ProjectDir: plain}, 0},
		{"repository checkout", session.Session{ProjectDir: repo}, 0},
		{"linked worktree", session.Session{ProjectDir: wt}, 1},
		{"tracking folder", session.Session{ProjectDir: plain, TrackingDir: track}, 1},
		{"both", session.Session{ProjectDir: wt, TrackingDir: track}, 2},
		{"none", session.Session{}, 0},
	}
	for _, c := range cases {
		s := c.s
		if got := sessionOwnedDirs(&s); len(got) != c.want {
			t.Errorf("%s: owned dirs = %v, want %d", c.name, got, c.want)
		}
	}
}

// A commit hook of another session runs from a worktree nested under the ended
// session's shared project folder (the pre-commit hook that died of signal 15
// while another session was closed). It survives the other session's end.
func TestSessionEndReap_NestedWorktreeHookOfOtherSessionSurvives(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	nested := filepath.Join(proj, "mw-other")
	if err := os.MkdirAll(filepath.Join(nested, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid := startScopeFixture(t, nested, filepath.Join(nested, "scripts", "check-docs-sync.sh"), "DATAWATCH_SESSION_ID=host-99998888")
	res := reapSessionEnd(&session.Session{FullID: "host-ab12cd34", ProjectDir: proj}, scopeReaper(), nil, nil)
	if processGone(pid) || res.Count != 0 {
		t.Fatalf("a hook of another session was ended: %+v", res)
	}
}
