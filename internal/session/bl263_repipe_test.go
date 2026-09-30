// BL263 / v6.11.9 — verify that ResumeMonitors re-establishes the
// tmux pipe-pane bridge for each surviving session at daemon startup.
//
// Operator report 2026-05-05: "When the server has restarted last few
// times i could not connect to the session again, I've had to stop and
// restart the session, like tmux or channel or something isn't working."
//
// Root cause: when the previous daemon died, the pipe-pane child tmux
// had spawned either died with the daemon or kept writing to a closed
// FD. Either way, the new daemon's monitor goroutine watched the log
// file via fsnotify but no new lines arrived because tmux was no
// longer piping. ResumeMonitors now calls RepipeOutput unconditionally
// on every surviving active session.

package session

import (
	"context"
	"testing"
	"time"
)

func TestBL263_ResumeMonitorsRepipesActiveSessions(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	// Seed two active sessions and one terminal one.
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
		LogFile: "/tmp/aa01.log",
	})
	_ = mgr.SaveSession(&Session{
		ID: "bb02", FullID: "testhost-bb02", TmuxSession: "cs-bb02",
		Hostname: "testhost", State: StateWaitingInput, UpdatedAt: time.Now(),
		LogFile: "/tmp/bb02.log",
	})
	_ = mgr.SaveSession(&Session{
		ID: "cc03", FullID: "testhost-cc03", TmuxSession: "cs-cc03",
		Hostname: "testhost", State: StateComplete, UpdatedAt: time.Now(),
		LogFile: "/tmp/cc03.log",
	})

	// Mark all three tmux sessions as alive in the fake.
	_ = fake.NewSessionWithSize("cs-aa01", 80, 24)
	_ = fake.NewSessionWithSize("cs-bb02", 80, 24)
	_ = fake.NewSessionWithSize("cs-cc03", 80, 24)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)

	// Both active sessions should have been re-piped; the completed
	// one is skipped (state filter excludes terminal states).
	if got := fake.Count("repipe"); got != 2 {
		t.Errorf("expected 2 repipe calls (one per active surviving session), got %d. Calls: %+v", got, fake.Calls)
	}
}

func TestBL263_ResumeMonitorsSkipsRepipeForDeadTmux(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	_ = fake // unused after the cleanup; kept for symmetry

	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
		LogFile: "/tmp/aa01.log",
	})
	// Tmux session is NOT alive — ResumeMonitors should mark it failed
	// and skip the repipe path entirely. (no NewSession call on fake.)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)

	if got := fake.Count("repipe"); got != 0 {
		t.Errorf("dead-tmux session should not be re-piped; got %d repipe calls. Calls: %+v", got, fake.Calls)
	}
}

// Regression test, v2: a subprocess/virtual session (schedule type=spawn,
// council, agent, etc.) has no TmuxSession — it was never tmux-backed.
// ResumeMonitors used to call tmux.SessionExists("") for these, which is
// always false, so any such session still StateRunning at daemon restart
// time was force-marked StateFailed even when its own runSubprocess
// goroutine would shortly (or already had) call subprocessFinish with a
// clean exit. That was fixed by skipping subprocess sessions entirely —
// but v8.36.13 found the skip over-corrected: ResumeMonitors runs ONLY at
// boot, so reaching a StateRunning subprocess session there means the
// daemon just restarted and the goroutine that would have called
// subprocessFinish on exit is unconditionally gone (unlike a tmux pane, a
// subprocess has no independent existence a new daemon generation can
// re-attach to). Silently skipping left the record — and any recurring
// schedule's overlap guard reading it — stuck forever. Observed in
// production: imap-hourly-rules-spawn skipped every hourly fire for ~12h
// straight after a subprocess run outlived a same-morning restart, even
// though the run itself had completed cleanly (output.log showed
// "[subprocess] completed (exit 0)") within two minutes of that restart.
// ResumeMonitors now marks it StateFailed here specifically, since boot
// time is the one context where "the goroutine is definitely gone" is
// known for certain rather than guessed at from a stale liveness check.
func TestResumeMonitorsMarksOrphanedSubprocessSessionsFailed(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	_ = fake

	_ = mgr.SaveSession(&Session{
		ID: "sp01", FullID: "testhost-sp01", TmuxSession: "",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
		LogFile: "/tmp/sp01.log",
	})

	// A recurring schedule's overlap guard (cmd/datawatch/main.go) is the
	// reason this needs to reach a terminal state, not just get corrected
	// silently — it reads exactly these onSessionEnd-style signals via
	// GetSession. Assert the callbacks actually fire so a listener (the
	// scheduler, in production) can react.
	var endCalls int
	mgr.SetOnSessionEnd(func(*Session) { endCalls++ })
	var stateChanges int
	mgr.SetStateChangeHandler(func(*Session, State) { stateChanges++ })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)

	sess, ok := mgr.store.Get("testhost-sp01")
	if !ok {
		t.Fatalf("session vanished from store")
	}
	if sess.State != StateFailed {
		t.Errorf("orphaned subprocess session (StateRunning at boot, no TmuxSession) left as %s; want StateFailed — its owning goroutine cannot possibly still be alive after a restart", sess.State)
	}
	if got := fake.Count("repipe"); got != 0 {
		t.Errorf("subprocess session has no tmux pane to repipe; got %d repipe calls. Calls: %+v", got, fake.Calls)
	}
	if endCalls != 1 {
		t.Errorf("onSessionEnd calls = %d, want 1 — a listener (e.g. a recurring schedule's overlap guard) needs this signal to stop treating the orphan as still in flight", endCalls)
	}
	if stateChanges != 1 {
		t.Errorf("onStateChange calls = %d, want 1", stateChanges)
	}
}
