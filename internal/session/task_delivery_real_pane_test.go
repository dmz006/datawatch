// Task delivery against recorded real Claude Code panes.
//
// The fixtures under testdata/delivery are real pane captures (see the README
// there). They carry what the hand-written panes of the older delivery tests
// leave out: a no-break space after the ❯ of the input row, a Channels notice
// above the box, a placeholder suggestion whose wording changes between CLI
// versions, and a box holding an unsent paste.
//
// Delivery must depend on what the screen shows (an empty, ready input box),
// not on the exact session state name, and must say why when it gives up.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestRealPane

package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rpPane reads one recorded pane fixture.
func rpPane(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "delivery", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// rpSave saves a one-shot claude-code session in the given state on the fake
// multiplexer, showing pane.
func rpSave(t *testing.T, mgr *Manager, fake *FakeTmux, id string, state State, pane string) *Session {
	t.Helper()
	sess := &Session{
		ID:            id,
		FullID:        "testhost-" + id,
		TmuxSession:   "cs-" + id,
		OneShot:       true,
		BackendFamily: "claude-code",
		State:         state,
		UpdatedAt:     time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	tdrSetPane(fake, sess.TmuxSession, pane)
	return sess
}

// The ready startup screens of two CLI versions (different placeholder text,
// no-break space after ❯, Channels notice above the box). The task must be
// typed exactly once whether the session state reads running or
// waiting_input: the state engine cannot tell an idle Claude from a busy one
// on a quiet pane, the screen can.
func TestRealPane_ReadyBoxDeliversWhateverTheStateName(t *testing.T) {
	panes := []string{
		"claude-2.1.331-startup-idle.txt",
		"claude-2.1.330-startup-idle-placeholder.txt",
	}
	for _, name := range panes {
		for _, st := range []State{StateWaitingInput, StateRunning} {
			t.Run(name+"/"+string(st), func(t *testing.T) {
				mgr, fake := newTestManagerWithFake(t)
				sess := rpSave(t, mgr, fake, "rp1", st, rpPane(t, name))
				tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
				if !mgr.ClaimTaskDelivery(sess.FullID) {
					t.Fatal("ClaimTaskDelivery lost the claim")
				}
				if err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 3, 60*time.Second, 5*time.Second); err != nil {
					t.Fatalf("DeliverOneShotTask in state %s: %v", st, err)
				}
				if !mgr.TaskDelivered(sess.FullID) {
					t.Error("TaskDelivered = false after a successful delivery")
				}
				if got := taskSends(fake, sess.TmuxSession, tdTask); got != 1 {
					t.Errorf("task sent %d times, want exactly 1", got)
				}
			})
		}
	}
}

// The development-channels consent dialog is not a ready box in any state,
// and the failure names the dialog instead of a generic "no main prompt".
func TestRealPane_DialogIsNeverReadyAndIsNamed(t *testing.T) {
	for _, st := range []State{StateWaitingInput, StateRunning} {
		t.Run(string(st), func(t *testing.T) {
			mgr, fake := newTestManagerWithFake(t)
			sess := rpSave(t, mgr, fake, "rp2", st, rpPane(t, "claude-2.1.331-dev-channels-dialog.txt"))
			tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
			var reason string
			mgr.SetOnTaskDeliveryFailed(func(_ string, r string) { reason = r })
			if !mgr.ClaimTaskDelivery(sess.FullID) {
				t.Fatal("ClaimTaskDelivery lost the claim")
			}
			err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 5*time.Second, time.Second)
			if !errors.Is(err, ErrTaskUndelivered) {
				t.Fatalf("error = %v, want ErrTaskUndelivered", err)
			}
			if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
				t.Errorf("task typed %d times over a dialog, want 0", got)
			}
			if !strings.Contains(strings.ToLower(reason), "dialog") {
				t.Errorf("failure reason %q does not name the dialog on screen", reason)
			}
			if strings.Contains(strings.ToLower(reason), "stalled") {
				t.Errorf("failure reason %q still says stalled", reason)
			}
		})
	}
}

// A box that already holds an unsent paste of the task is not ready: pasting
// again is what piled up dozens of "[Pasted text" chips. The failure says the
// box holds text.
func TestRealPane_BoxHoldingUnsentPasteIsNotReady(t *testing.T) {
	for _, st := range []State{StateWaitingInput, StateRunning} {
		t.Run(string(st), func(t *testing.T) {
			mgr, fake := newTestManagerWithFake(t)
			sess := rpSave(t, mgr, fake, "rp3", st, rpPane(t, "claude-2.1.330-unsubmitted-paste.txt"))
			tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
			var reason string
			mgr.SetOnTaskDeliveryFailed(func(_ string, r string) { reason = r })
			if !mgr.ClaimTaskDelivery(sess.FullID) {
				t.Fatal("ClaimTaskDelivery lost the claim")
			}
			err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 5*time.Second, time.Second)
			if !errors.Is(err, ErrTaskUndelivered) {
				t.Fatalf("error = %v, want ErrTaskUndelivered", err)
			}
			if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
				t.Errorf("task pasted %d more times into a box that already holds text, want 0", got)
			}
			if !strings.Contains(reason, "input box already holds text") {
				t.Errorf("failure reason %q does not say the box holds text", reason)
			}
		})
	}
}

// A pane that shows work (spinner, running tool, "esc to interrupt") is not
// ready even when the session state says running or waiting: nothing is typed
// into a busy turn.
func TestRealPane_BusyPaneIsNotReady(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := rpSave(t, mgr, fake, "rp4", StateWaitingInput, rpPane(t, "claude-2.1.331-working.txt"))
	tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
	var reason string
	mgr.SetOnTaskDeliveryFailed(func(_ string, r string) { reason = r })
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 5*time.Second, time.Second)
	if !errors.Is(err, ErrTaskUndelivered) {
		t.Fatalf("error = %v, want ErrTaskUndelivered", err)
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
		t.Errorf("task typed %d times into a working turn, want 0", got)
	}
	if !strings.Contains(reason, "working") {
		t.Errorf("failure reason %q does not say the agent is working", reason)
	}
}

// The busy probe must read an unsent paste in the box as an idle prompt: the
// wrapped chips push the ❯ row out of the last three lines, and an idle Claude
// always has MCP helper children, so the old rule held such a session at
// running for as long as the paste sat there.
func TestRealPane_BusyProbeReadsWrappedBoxAsIdle(t *testing.T) {
	pane := rpPane(t, "claude-2.1.330-unsubmitted-paste.txt")
	if paneLooksBusy(pane, true) {
		t.Error("paneLooksBusy = true for an idle box holding wrapped paste chips")
	}
	if !paneLooksBusy(rpPane(t, "claude-2.1.331-working.txt"), false) {
		t.Error("paneLooksBusy = false for a working pane")
	}
}

// The recorded startup screen is a ready input box; the dialog and the
// working screens are not. Pure screen classification, no state involved.
func TestRealPane_InputBoxClassification(t *testing.T) {
	cases := []struct {
		fixture string
		want    inputBoxKind
	}{
		{"claude-2.1.331-startup-idle.txt", inputBoxEmpty},
		{"claude-2.1.330-startup-idle-placeholder.txt", inputBoxEmpty},
		{"claude-2.1.330-unsubmitted-paste.txt", inputBoxHoldsText},
		{"claude-2.1.331-dev-channels-dialog.txt", inputBoxDialog},
		{"claude-2.1.331-working.txt", inputBoxBusy},
	}
	for _, c := range cases {
		got, _ := classifyInputBox(rpPane(t, c.fixture), tdTask)
		if got != c.want {
			t.Errorf("%s: classifyInputBox = %v, want %v", c.fixture, got, c.want)
		}
	}
	// Placeholder wording is free: any text that is neither a paste chip nor
	// the task itself is a suggestion.
	for _, ph := range []string{`Try "fix lint errors"`, `Ask me anything about this repo`, ``} {
		pane := "▝▜██████▀  Claude Code\n──────────\n❯ " + ph + "\n──────────\n  ⏵⏵ bypass permissions on\n"
		if got, _ := classifyInputBox(pane, tdTask); got != inputBoxEmpty {
			t.Errorf("placeholder %q: classifyInputBox = %v, want ready", ph, got)
		}
	}
}

// The failure reason of a delivery that gave up is kept per session so the
// launch that ends because of it can say why instead of "worker stalled".
func TestRealPane_FailureReasonIsKeptForTheLaunch(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := rpSave(t, mgr, fake, "rp5", StateRunning, rpPane(t, "claude-2.1.330-unsubmitted-paste.txt"))
	tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
	if got := mgr.TaskDeliveryFailure(sess.FullID); got != "" {
		t.Fatalf("TaskDeliveryFailure before any failure = %q, want empty", got)
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	if err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 1, 3*time.Second, time.Second); !errors.Is(err, ErrTaskUndelivered) {
		t.Fatalf("error = %v, want ErrTaskUndelivered", err)
	}
	if got := mgr.TaskDeliveryFailure(sess.FullID); !strings.Contains(got, "input box already holds text") {
		t.Errorf("TaskDeliveryFailure = %q, want the box-holds-text reason", got)
	}
}

// The daemon starts delivery itself when a claude session has a task: the
// bridge's ready call is a bonus, not the only trigger. With no ready call at
// all the task still reaches the ready box exactly once, and a late ready call
// finds the claim taken.
func TestRealPane_DeliveryStartsWithoutChannelReady(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := rpSave(t, mgr, fake, "rp6", StateRunning, rpPane(t, "claude-2.1.331-startup-idle.txt"))
	sess.Task = tdTask
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
	mgr.armTaskDelivery(sess.FullID, 3, 60*time.Second, 5*time.Second)
	if !mgr.TaskDelivered(sess.FullID) {
		t.Fatal("task never delivered without a channel ready call")
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1", got)
	}
	if mgr.ClaimTaskDelivery(sess.FullID) {
		t.Error("a late ready call could claim the task again and type it twice")
	}
}
