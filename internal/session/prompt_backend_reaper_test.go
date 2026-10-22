// The one-shot reaper closes a finished prompt-backend one-shot: it lists
// the session, completes it and does so exactly once. The guard subtests pin
// the reaper's two exclusions: interactive-backend sessions are never reaped,
// and the pi failure marker is never counted as a completion.

package session

import (
	"context"
	"testing"
)

func TestReaperEndsFinishedPromptSession(t *testing.T) {
	isolateMultiplexerEnv(t)
	mgr, fake := newPromptFamilyTestManager(t)

	// Started without an explicit OneShot option: the creation-time
	// decision for the prompt backend is what makes it one-shot.
	started, err := mgr.Start(context.Background(), "one-shot pi job", "", t.TempDir(), &StartOptions{
		Backend:  "pi-prompt",
		LaunchFn: stubPromptLaunch(t, fake),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sess, ok := mgr.GetSession(started.FullID)
	if !ok {
		t.Fatalf("started session %q not found via GetSession", started.FullID)
	}
	if !sess.OneShot {
		t.Fatalf("precondition: pi-prompt session must be one-shot by the creation decision")
	}
	if sess.TmuxSession == "" {
		t.Fatalf("precondition: started session has no multiplexer session name")
	}

	// The worker returned: the completion marker sits on a line of its own,
	// at the start of the line — not as the tail of an echoed command.
	fake.Pane[sess.TmuxSession] = "worker output\n" + oneShotCompletionMarker + " pi job done\n"

	// (a) The reaper's listing contains the session.
	finished := mgr.FinishedOneShots()
	if len(finished) != 1 || finished[0].FullID != sess.FullID {
		t.Fatalf("FinishedOneShots = %+v, want exactly %s", finished, sess.FullID)
	}

	// (b) Reaping returns its id and the stored session reaches complete.
	reaped := mgr.ReapFinishedOneShots()
	if len(reaped) != 1 || reaped[0] != sess.FullID {
		t.Fatalf("ReapFinishedOneShots = %v, want [%s]", reaped, sess.FullID)
	}
	stored, ok := mgr.GetSession(sess.FullID)
	if !ok {
		t.Fatalf("stored session %q missing after reap", sess.FullID)
	}
	if stored.State != StateComplete {
		t.Errorf("state after reap = %q, want %q", stored.State, StateComplete)
	}

	// (c) Reaping is idempotent: a second pass finds nothing.
	if again := mgr.ReapFinishedOneShots(); len(again) != 0 {
		t.Errorf("second reap = %v, want none (already completed)", again)
	}

	// Guard: an interactive session whose pane shows the same completion
	// marker is never the reaper's to list or close.
	t.Run("interactive-backend-not-listed-or-reaped", func(t *testing.T) {
		isolateMultiplexerEnv(t)
		mgr, fake := newPromptFamilyTestManager(t)

		started, err := mgr.Start(context.Background(), "interactive job", "", t.TempDir(), &StartOptions{
			Backend:  "claude-code", // interactive; OneShot left at its zero value
			LaunchFn: stubPromptLaunch(t, fake),
		})
		if err != nil {
			t.Fatalf("Start(claude-code): %v", err)
		}
		sess, ok := mgr.GetSession(started.FullID)
		if !ok {
			t.Fatalf("started session %q not found via GetSession", started.FullID)
		}
		if sess.OneShot {
			t.Fatalf("precondition: claude-code session must be interactive (OneShot=false)")
		}
		fake.Pane[sess.TmuxSession] = oneShotCompletionMarker + " looks done\n"

		if finished := mgr.FinishedOneShots(); len(finished) != 0 {
			t.Errorf("FinishedOneShots = %+v, want none for an interactive session", finished)
		}
		if reaped := mgr.ReapFinishedOneShots(); len(reaped) != 0 {
			t.Errorf("ReapFinishedOneShots = %v, want none for an interactive session", reaped)
		}
		stored, ok := mgr.GetSession(sess.FullID)
		if !ok {
			t.Fatalf("stored session %q missing", sess.FullID)
		}
		if stored.State == StateComplete {
			t.Errorf("interactive session must not be completed by the reaper; state = %q", stored.State)
		}
	})

	// Guard: the pi failure marker on its own line is a failure signal,
	// never a completion — it must not be reaped as a success.
	t.Run("pi-failure-marker-not-reaped-as-success", func(t *testing.T) {
		isolateMultiplexerEnv(t)
		mgr, fake := newPromptFamilyTestManager(t)

		started, err := mgr.Start(context.Background(), "failing pi job", "", t.TempDir(), &StartOptions{
			Backend:  "pi-prompt",
			LaunchFn: stubPromptLaunch(t, fake),
		})
		if err != nil {
			t.Fatalf("Start(pi-prompt): %v", err)
		}
		sess, ok := mgr.GetSession(started.FullID)
		if !ok {
			t.Fatalf("started session %q not found via GetSession", started.FullID)
		}
		if !sess.OneShot {
			t.Fatalf("precondition: pi-prompt session must be one-shot by the creation decision")
		}
		fake.Pane[sess.TmuxSession] = "worker output\n" + piFailureMarker + " pi exited with status 1\n"

		if finished := mgr.FinishedOneShots(); len(finished) != 0 {
			t.Errorf("FinishedOneShots = %+v, want none for a failed pi run", finished)
		}
		if reaped := mgr.ReapFinishedOneShots(); len(reaped) != 0 {
			t.Errorf("ReapFinishedOneShots = %v, want none — the failure marker is not a success", reaped)
		}
		stored, ok := mgr.GetSession(sess.FullID)
		if !ok {
			t.Fatalf("stored session %q missing", sess.FullID)
		}
		if stored.State == StateComplete {
			t.Errorf("a failed pi run must not be recorded as complete; state = %q", stored.State)
		}
	})
}
