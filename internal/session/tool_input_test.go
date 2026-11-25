// Input from MCP tools follows the prompt rules of the daemon's own senders.

package session

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// guardedFake is a FakeTmux with the real layer's re-check between the typed
// text and Enter. afterText runs once the text is in the pane.
type guardedFake struct {
	*FakeTmux
	afterText func()
}

func (g guardedFake) SendKeysGuarded(session, keys string, settle time.Duration, veto func(pane string) bool) error {
	if err := g.record("send-keys-text", session, keys); err != nil {
		return err
	}
	if g.afterText != nil {
		g.afterText()
	}
	return submitUnlessVetoed(g.FakeTmux, session, keys, veto)
}

const toolIdlePane = "⏺ Done.\n\n  ? for shortcuts\n"

func TestToolInputRereadsScreenBeforeEnter(t *testing.T) {
	cases := map[string]struct {
		after func(f *FakeTmux)
		say   string
	}{
		"prompt appears": {func(f *FakeTmux) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.Pane["cs-ab12"] = approvalPane
		}, "appeared while the text was being typed"},
		"screen unreadable": {func(f *FakeTmux) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.FailNextOp = map[string]int{"capture-live-tail": 1}
		}, "could not be re-read"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			mgr, f, sess := newPromptSession(t, toolIdlePane)
			mgr.tmux = guardedFake{FakeTmux: f, afterText: func() { c.after(f) }}
			var events []InputAuditEvent
			mgr.SetInputAuditHook(func(ev InputAuditEvent) { events = append(events, ev) })
			err := mgr.SendToolInput(sess.FullID, "yes", "mcp")
			if !errors.Is(err, ErrPromptOnScreen) {
				t.Fatalf("err = %v, want a prompt refusal", err)
			}
			if f.Count("enter") != 0 {
				t.Fatal("Enter was pressed")
			}
			msg := err.Error()
			if !strings.Contains(msg, c.say) || !strings.Contains(msg, "Enter was not pressed") || !strings.Contains(msg, "waiting for the operator") {
				t.Errorf("refusal = %q", msg)
			}
			// The text is already in the session: do not claim otherwise.
			if strings.Contains(msg, "nothing was typed") {
				t.Errorf("refusal claims nothing was typed: %q", msg)
			}
			if len(events) != 1 || events[0].Action != InputAuditRefused || events[0].Source != "mcp" || events[0].Reason != msg {
				t.Fatalf("audit events = %+v", events)
			}
			if got, _ := mgr.GetSession(sess.FullID); got.InputCount != 0 {
				t.Errorf("a refused send was counted as input")
			}
		})
	}
}

func TestToolInputWithoutPromptIsSubmittedAndRecordedAsBefore(t *testing.T) {
	mgr, f, sess := newPromptSession(t, toolIdlePane)
	mgr.tmux = guardedFake{FakeTmux: f}
	if err := mgr.SendToolInput(sess.FullID, "carry on", "mcp"); err != nil {
		t.Fatal(err)
	}
	if f.Count("enter") != 1 {
		t.Fatalf("enters = %d, want 1", f.Count("enter"))
	}
	got, _ := mgr.GetSession(sess.FullID)
	if got.InputCount != 1 || got.LastInput != "carry on" || got.State != StateRunning {
		t.Fatalf("recorded count=%d last=%q state=%s", got.InputCount, got.LastInput, got.State)
	}
}

// The rules belong to the tool entry point. REST callers choose their source
// name, "mcp" included, and what they send is not held back.
func TestRESTSourceNamedMCPIsNotHeldToToolRules(t *testing.T) {
	mgr, f, sess := newPromptSession(t, approvalPane)
	if IsAutomatedInputSource("mcp") {
		t.Fatal(`source "mcp" must not become an automated source`)
	}
	if err := mgr.SendInput(sess.FullID, "1", "mcp"); err != nil {
		t.Fatal(err)
	}
	if len(typedCalls(f)) == 0 {
		t.Fatal("nothing typed")
	}
}

// deadLivePane reads the visible pane but loses the live screen after the
// first read.
type deadLivePane struct {
	*FakeTmux
	reads *int
}

func (d deadLivePane) CapturePaneLiveTail(s string) (string, error) {
	*d.reads++
	if *d.reads > 1 {
		return "", ErrInjected
	}
	return d.CapturePaneVisible(s)
}

// A guarded send presses no retry Enter once the live screen is unreadable.
func TestGuardedSubmitDoesNotRetryOnUnreadableScreen(t *testing.T) {
	oldSleep := pasteSleep
	pasteSleep = func(time.Duration) {}
	defer func() { pasteSleep = oldSleep }()

	const text = "line one\nline two"
	unsent := "❯ [Pasted text #1 +2 lines]\n"
	f := NewFakeTmux()
	f.Pane["s"] = unsent
	reads := 0
	if err := submitUnlessVetoed(deadLivePane{f, &reads}, "s", text, paneShowsApprovalMenu); err != nil {
		t.Fatal(err)
	}
	if n := f.Count("enter"); n != 1 {
		t.Fatalf("enters = %d, want only the first", n)
	}
}
