// v9.0.7 — self-heal a session stranded StateFailed by a bad boot when its
// tmux pane is actually still alive.
//
// Incident, 2026-10-09: `datawatch restart`'s old daemonize() path started
// a fresh daemon that never inherited TMUX_TMPDIR, so every tmux call hit
// the wrong (default, no-server) socket. ResumeMonitors only ever looked at
// sessions already in StateRunning/StateWaitingInput/StateRateLimited, so
// the resulting SessionExists false negatives marked 3 genuinely-alive
// sessions StateFailed, and nothing at any later boot would ever look at
// them again — an operator had to hand-edit sessions.json while the daemon
// was stopped to bring them back. ResumeMonitors now also considers
// StateFailed sessions whose tmux pane is still alive on the resolved
// socket, and recovers them to StateRunning. Deliberately scoped to
// StateFailed only — see the boundary test below for why StateKilled and
// StateComplete must NOT get the same treatment.
package session

import (
	"context"
	"testing"
	"time"
)

// TestV907_ResumeMonitorsRecoversFailedSessionWithLiveTmuxPane reproduces
// the exact incident case: a session marked failed by a prior bad boot,
// whose tmux pane never actually died, must come back as running at the
// next boot — not stay stranded until an operator edits the store by hand.
func TestV907_ResumeMonitorsRecoversFailedSessionWithLiveTmuxPane(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	_ = mgr.SaveSession(&Session{
		ID: "ab22", FullID: "testhost-ab22", TmuxSession: "cs-testhost-ab22",
		Hostname: "testhost", State: StateFailed, UpdatedAt: time.Now(),
		LogFile: "/tmp/ab22.log",
	})
	// The pane is alive the whole time — this is the false-negative case,
	// not a genuinely dead session.
	_ = fake.NewSessionWithSize("cs-testhost-ab22", 80, 24)

	var stateChanges int
	var lastOld State
	mgr.SetStateChangeHandler(func(_ *Session, old State) {
		stateChanges++
		lastOld = old
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)

	sess, ok := mgr.store.Get("testhost-ab22")
	if !ok {
		t.Fatalf("session vanished from store")
	}
	if sess.State != StateRunning {
		t.Fatalf("session left as %s; want StateRunning — its tmux pane is alive, so it never actually died", sess.State)
	}
	if stateChanges != 1 {
		t.Errorf("onStateChange calls = %d, want 1 (failed -> running)", stateChanges)
	}
	if lastOld != StateFailed {
		t.Errorf("onStateChange old state = %s, want %s", lastOld, StateFailed)
	}
	// Recovery must also re-establish the pipe-pane bridge like a normal
	// surviving session would, not just flip the state field.
	if got := fake.Count("repipe"); got != 1 {
		t.Errorf("expected 1 repipe call for the recovered session, got %d. Calls: %+v", got, fake.Calls)
	}
}

// TestV907_ResumeMonitorsLeavesTrulyDeadFailedSessionAlone is the negative
// case: a session correctly marked failed, whose tmux pane is genuinely
// gone, must stay failed — the self-heal only applies when there's live
// evidence the session never actually died.
func TestV907_ResumeMonitorsLeavesTrulyDeadFailedSessionAlone(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	_ = fake

	_ = mgr.SaveSession(&Session{
		ID: "dead1", FullID: "testhost-dead1", TmuxSession: "cs-testhost-dead1",
		Hostname: "testhost", State: StateFailed, UpdatedAt: time.Now(),
		LogFile: "/tmp/dead1.log",
	})
	// No fake.NewSessionWithSize call — the tmux pane genuinely doesn't exist.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)

	sess, ok := mgr.store.Get("testhost-dead1")
	if !ok {
		t.Fatalf("session vanished from store")
	}
	if sess.State != StateFailed {
		t.Errorf("session left as %s; want StateFailed unchanged — its tmux pane is genuinely gone", sess.State)
	}
	if got := fake.Count("repipe"); got != 0 {
		t.Errorf("a genuinely dead session must not be re-piped; got %d repipe calls. Calls: %+v", got, fake.Calls)
	}
}

// TestV907_ResumeMonitorsDoesNotRecoverKilledOrCompleteSessions is the
// boundary case: StateKilled and StateComplete, even with a live tmux
// pane, must be left exactly alone. Both are set by legitimate code paths
// unrelated to a tmux-liveness false negative — explicit operator kill
// (KillSession kills the tmux pane itself, so a still-alive pane there is
// already an anomaly, not evidence the kill should be undone) and normal
// completion detection. Resurrecting either would override real intent
// instead of undoing a boot-time glitch. This also matches BL263's
// pre-existing expectation that a completed session is never re-piped.
func TestV907_ResumeMonitorsDoesNotRecoverKilledOrCompleteSessions(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)

	_ = mgr.SaveSession(&Session{
		ID: "kk01", FullID: "testhost-kk01", TmuxSession: "cs-testhost-kk01",
		Hostname: "testhost", State: StateKilled, UpdatedAt: time.Now(),
		LogFile: "/tmp/kk01.log",
	})
	_ = mgr.SaveSession(&Session{
		ID: "cc01", FullID: "testhost-cc01", TmuxSession: "cs-testhost-cc01",
		Hostname: "testhost", State: StateComplete, UpdatedAt: time.Now(),
		LogFile: "/tmp/cc01.log",
	})
	_ = fake.NewSessionWithSize("cs-testhost-kk01", 80, 24)
	_ = fake.NewSessionWithSize("cs-testhost-cc01", 80, 24)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.ResumeMonitors(ctx)

	wantState := map[string]State{"testhost-kk01": StateKilled, "testhost-cc01": StateComplete}
	for id, want := range wantState {
		sess, ok := mgr.store.Get(id)
		if !ok {
			t.Fatalf("%s vanished from store", id)
		}
		if sess.State != want {
			t.Errorf("%s left as %s; want unchanged %s", id, sess.State, want)
		}
	}
	if got := fake.Count("repipe"); got != 0 {
		t.Errorf("killed/complete sessions must not be re-piped; got %d repipe calls. Calls: %+v", got, fake.Calls)
	}
}
