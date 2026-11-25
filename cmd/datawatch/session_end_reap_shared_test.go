package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// linkedWorktree makes dir look like a linked git worktree (a .git FILE).
func linkedWorktree(t *testing.T) string {
	t.Helper()
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /nonexistent/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return wt
}

func reapTestManager(t *testing.T, sessions ...*session.Session) *session.Manager {
	t.Helper()
	mgr, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.WithFakeTmux()
	for _, s := range sessions {
		s.Hostname, s.UpdatedAt = "testhost", time.Now()
		if err := mgr.SaveSession(s); err != nil {
			t.Fatal(err)
		}
	}
	return mgr
}

// aliveAfter gives a wrongly sent signal time to take effect.
func aliveAfter(pid int) bool { return !waitGone(pid, time.Now().Add(500*time.Millisecond)) }

// Two sessions work in one linked worktree. Ending one must not reach the
// other: not its processes, and not a script parked in the folder they share.
func TestSessionEndReap_SharedLinkedWorktreeOtherSessionScriptSurvives(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	wt := linkedWorktree(t)
	ending := &session.Session{ID: "ab12", FullID: "testhost-ab12", State: session.StateRunning, ProjectDir: wt}
	other := &session.Session{ID: "cd34", FullID: "testhost-cd34", State: session.StateRunning, ProjectDir: wt}
	mgr := reapTestManager(t, ending, other)
	wireSessionEndReap(mgr)

	theirs := startScopeFixture(t, wt, filepath.Join(wt, "their-hook.sh"), "DATAWATCH_SESSION_ID="+other.FullID)
	unmarked := startScopeFixture(t, wt, filepath.Join(wt, "their-watcher.sh"))
	own := startScopeFixture(t, wt, filepath.Join(wt, "own.sh"), "DATAWATCH_SESSION_ID="+ending.FullID)

	if err := mgr.EndSession(ending.FullID, session.EndReason{Caller: "test", Reason: "operator kill"}); err != nil {
		t.Fatal(err)
	}
	if !aliveAfter(theirs) {
		t.Error("the other session's hook in the shared worktree was ended")
	}
	if !aliveAfter(unmarked) {
		t.Error("a script in the shared worktree with no proof of ownership was ended")
	}
	if !waitGone(own, time.Now().Add(3*time.Second)) {
		t.Error("the ended session's own process survived")
	}
}

// A process of a live session survives even when it sits in a worktree that
// only the ended session is recorded in. Its id may be the short one.
func TestSessionEndReap_LiveSessionProcessInEndedWorktreeSurvives(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"full id", "short id"} {
		wt := linkedWorktree(t)
		ending := &session.Session{ID: "ab12", FullID: "testhost-ab12", State: session.StateRunning, ProjectDir: wt}
		other := &session.Session{ID: "cd34", FullID: "testhost-cd34", State: session.StateWaitingInput, ProjectDir: t.TempDir()}
		mgr := reapTestManager(t, ending, other)
		wireSessionEndReap(mgr)
		ref := other.FullID
		if name == "short id" {
			ref = other.ID
		}
		theirs := startScopeFixture(t, wt, filepath.Join(wt, "their.sh"), "DATAWATCH_SESSION_ID="+ref)
		leftover := startScopeFixture(t, wt, filepath.Join(wt, "left.sh"))

		if err := mgr.EndSession(ending.FullID, session.EndReason{Caller: "test", Reason: "operator kill"}); err != nil {
			t.Fatal(err)
		}
		if !aliveAfter(theirs) {
			t.Errorf("%s: a live session's process was ended by another session's end", name)
		}
		if !waitGone(leftover, time.Now().Add(3*time.Second)) {
			t.Errorf("%s: a leftover in the ended session's own worktree survived", name)
		}
	}
}

// reapModeCheck is the host load check in reap mode with a breach that has
// lasted long enough to act.
func reapModeCheck(sessions []*session.Session) autonomouspkg.HostLoadResult {
	check := newHostLoadCheck(
		func() autonomouspkg.Config {
			return autonomouspkg.Config{HostLoadAlert: config.HostLoadAlertConfig{Mode: "reap", Minutes: 1}}
		},
		func() []*session.Session { return sessions },
		func(string, string) {},
	)
	now := time.Now()
	check.Now = func() time.Time { return now }
	check.Load = func() (float64, error) { return 100, nil }
	check.Cores = func() int { return 4 }
	check.Reaper = scopeReaper()
	check.Check()
	now = now.Add(2 * time.Minute)
	return check.Check()
}

// The shared project folder of an ended session proves nothing: the host load
// reap must not end what runs there.
func TestHostLoadReap_EndedSessionSharedFolderIsNotEvidence(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	pid := startScopeFixture(t, proj, filepath.Join(proj, "operator-watch.sh"))
	res := reapModeCheck([]*session.Session{
		{FullID: "testhost-ee01", State: session.StateComplete, ProjectDir: proj},
	})
	if !aliveAfter(pid) {
		t.Fatalf("a script in a shared project folder was ended by the host load reap: %+v", res.Reaped)
	}
}

// A linked worktree a running session also works in is shared too.
func TestHostLoadReap_WorktreeUsedByRunningSessionIsNotEvidence(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	wt := linkedWorktree(t)
	pid := startScopeFixture(t, wt, filepath.Join(wt, "their.sh"))
	res := reapModeCheck([]*session.Session{
		{FullID: "testhost-ee01", State: session.StateComplete, ProjectDir: wt},
		{FullID: "testhost-ee02", State: session.StateRunning, ProjectDir: wt},
	})
	if !aliveAfter(pid) {
		t.Fatalf("a process in a worktree a running session uses was ended: %+v", res.Reaped)
	}
}

// An ended session's own linked worktree is still cleaned, except for a running
// session's process that happens to run from it.
func TestHostLoadReap_OwnWorktreeStillReapedButNotARunningSessionsProcess(t *testing.T) {
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	wt := linkedWorktree(t)
	left := startScopeFixture(t, wt, filepath.Join(wt, "left.sh"), "DATAWATCH_SESSION_ID=testhost-ee01")
	theirs := startScopeFixture(t, wt, filepath.Join(wt, "their.sh"), "DATAWATCH_SESSION_ID=testhost-ee02")
	res := reapModeCheck([]*session.Session{
		{FullID: "testhost-ee01", State: session.StateComplete, ProjectDir: wt},
		{FullID: "testhost-ee02", State: session.StateRunning, ProjectDir: t.TempDir()},
	})
	if !aliveAfter(theirs) {
		t.Errorf("a running session's process was ended: %+v", res.Reaped)
	}
	if !waitGone(left, time.Now().Add(3*time.Second)) {
		t.Errorf("the ended session's leftover in its own worktree survived: %+v", res.Reaped)
	}
}
