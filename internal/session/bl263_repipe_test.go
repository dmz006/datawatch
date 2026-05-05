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
