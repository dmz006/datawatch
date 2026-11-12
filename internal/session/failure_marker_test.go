// BL468 — deterministic fake-multiplexer tests for the session package's
// non-zero pi status handling (StartScreenCapture's failure branch, added
// next to the completion scans in manager.go).
//
// Seam: the one-shot sessions below are driven through StartScreenCapture,
// the same loop neighboring tests (startup_dialog_dismiss_test.go,
// self_update_dialog_test.go) use, with the in-memory FakeTmux standing in
// for the terminal multiplexer — no live multiplexer, no network, no sleeps
// beyond the bounded pollUntil/scanTicks helpers the neighboring tests use.
//
//  1. a genuine "DATAWATCH_PI_FAILED: pi exited with status <N>" pane line
//     transitions a StateRunning one-shot to StateFailed with
//     FailureExitCode == N — and never to StateComplete
//  2. the same marker text as a pane echo of the typed launch command
//     (wrapped tail ending in the closing quote — the case
//     isRealCompletionLine rejects for completion lines, mirrored by
//     isRealFailureLine, and inside the echoed task region that echoGate
//     excludes) must NOT mark the session failed
//  3. a genuine DATAWATCH_COMPLETE line still transitions the one-shot to
//     StateComplete — the new failure branch must not leak into the
//     success path

package session

import (
	"testing"
	"time"
)

const piFailedLine = "DATAWATCH_PI_FAILED: pi exited with status 3 (model prov/model-x)"

// newPiOneShotSession saves a one-shot pi session in StateRunning with the
// given task (the echo guards compare the pane against this text) and
// returns it.
func newPiOneShotSession(t *testing.T, mgr *Manager, id string, task string) *Session {
	t.Helper()
	sess := &Session{
		ID:            id,
		FullID:        "testhost-" + id,
		Hostname:      "testhost",
		TmuxSession:   "cs-" + id,
		BackendFamily: "pi",
		OneShot:       true,
		State:         StateRunning,
		Task:          task,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return sess
}

func storeState(t *testing.T, m *Manager, fullID string) *Session {
	t.Helper()
	s, ok := m.store.Get(fullID)
	if !ok {
		t.Fatalf("session %s disappeared from the store", fullID)
	}
	return s
}

// TestOneShotFailureMarkerMarksSessionFailed — a one-shot pi session in
// StateRunning whose pane shows the launch wrapper's non-zero-exit line must
// end StateFailed with FailureExitCode 3, and may never be assigned
// StateComplete.
func TestOneShotFailureMarkerMarksSessionFailed(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newPiOneShotSession(t, mgr, "pf01", "run the pi task and report the result")

	fake.Pane[sess.TmuxSession] = "pi is working on the task\n" + piFailedLine + "\n"

	stop := startCapture(t, mgr, sess)
	pollUntil(t, 10*time.Second, func() bool {
		s, ok := mgr.store.Get(sess.FullID)
		return ok && s.State == StateFailed
	}, "failure marker line never transitioned the one-shot session to StateFailed")
	stop()

	cur := storeState(t, mgr, sess.FullID)
	if cur.State == StateComplete {
		t.Fatalf("non-zero pi status must not mark the session StateComplete; got StateComplete (FailureExitCode=%d)", cur.FailureExitCode)
	}
	if cur.State != StateFailed {
		t.Fatalf("session state = %q, want StateFailed", cur.State)
	}
	if cur.FailureExitCode != 3 {
		t.Fatalf("FailureExitCode = %d, want 3 (from \"pi exited with status 3\")", cur.FailureExitCode)
	}
}

// TestFailureMarkerEchoDoesNotMarkFailed — the marker text may be on the
// pane as a wrapper fragment of the TYPED LAUNCH COMMAND (the terminal
// wrapped the command and the last fragment starts with the marker and
// ends with the closing quote), not as the wrapper's own output. The guards
// (isRealFailureLine's isShellSourceTail tail check, plus echoGate with
// the task text located inside the pane) must both reject it: the session
// stays StateRunning with FailureExitCode unset.
func TestFailureMarkerEchoDoesNotMarkFailed(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	typedCommand := "run pi and if non-zero: echo '" + piFailedLine + "'" // marker text inside the quoted command
	sess := newPiOneShotSession(t, mgr, "pe01", typedCommand)

	// The terminal wrapped the echoed command: the continuation line starts
	// exactly with the marker and ends with the closing quote of the typed
	// argument — the wrapped-command tail isShellSourceTail rejects.
	fake.Pane[sess.TmuxSession] =
		"bash -lc 'run pi and if non-zero: echo '\n" + piFailedLine + "'\n"

	stop := startCapture(t, mgr, sess)
	scanTicks(t, sess, fake, 3) // prove the loop scanned the echoed pane several times
	stop()

	cur := storeState(t, mgr, sess.FullID)
	if cur.State != StateRunning {
		t.Fatalf("echoed command text containing the marker must not change the state; got %q (FailureExitCode=%d), want StateRunning", cur.State, cur.FailureExitCode)
	}
	if cur.FailureExitCode != 0 {
		t.Fatalf("echoed marker text must not record an exit code; FailureExitCode = %d, want 0 (unset)", cur.FailureExitCode)
	}
}

// TestOneShotCompletionStillMarksComplete — the success path is unaffected
// by the new failure branch: a genuine DATAWATCH_COMPLETE line still
// transitions the StateRunning one-shot to StateComplete, and does not
// touch FailureExitCode.
func TestOneShotCompletionStillMarksComplete(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newPiOneShotSession(t, mgr, "pc01", "run the pi task and report back")

	fake.Pane[sess.TmuxSession] = "pi finished the run\nDATAWATCH_COMPLETE: pi done\n"

	stop := startCapture(t, mgr, sess)
	pollUntil(t, 10*time.Second, func() bool {
		s, ok := mgr.store.Get(sess.FullID)
		return ok && s.State == StateComplete
	}, "DATAWATCH_COMPLETE line never transitioned the one-shot session to StateComplete")
	stop()

	cur := storeState(t, mgr, sess.FullID)
	if cur.State != StateComplete {
		t.Fatalf("session state = %q, want StateComplete (FailureExitCode=%d)", cur.State, cur.FailureExitCode)
	}
	if cur.FailureExitCode != 0 {
		t.Fatalf("completed session must not carry a failure exit code; FailureExitCode = %d, want 0 (unset)", cur.FailureExitCode)
	}
}
