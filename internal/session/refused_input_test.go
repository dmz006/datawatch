// Which SendInput errors prove that nothing was typed, checked through the
// real send path.

package session

import (
	"errors"
	"testing"
)

func TestRefusedBeforeTypingOnlyWhenNothingReachedThePane(t *testing.T) {
	t.Run("a prompt is on screen at the first look", func(t *testing.T) {
		mgr, f, sess := newPromptSession(t, approvalPane)
		err := mgr.SendInput(sess.FullID, "carry on", "automaton")
		if !RefusedBeforeTyping(err) {
			t.Fatalf("err = %v, want proof that nothing was typed", err)
		}
		if n := len(typedCalls(f)); n != 0 || f.Count("send-keys-text") != 0 {
			t.Fatalf("keys reached the pane: %v", typedCalls(f))
		}
	})
	t.Run("the pane cannot be read at the first look", func(t *testing.T) {
		mgr, f, sess := newPromptSession(t, toolIdlePane)
		f.FailNextOp = map[string]int{"capture-live-tail": 1}
		err := mgr.SendInput(sess.FullID, "carry on", "supervisor-wake")
		if !RefusedBeforeTyping(err) {
			t.Fatalf("err = %v, want proof that nothing was typed", err)
		}
		if n := len(typedCalls(f)); n != 0 {
			t.Fatalf("keys reached the pane: %v", typedCalls(f))
		}
	})
	t.Run("a prompt appears after the text was typed", func(t *testing.T) {
		mgr, f, sess := newPromptSession(t, toolIdlePane)
		mgr.tmux = guardedFake{FakeTmux: f, afterText: func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.Pane[sess.TmuxSession] = approvalPane
		}}
		err := mgr.SendInput(sess.FullID, "carry on", "automaton")
		if !errors.Is(err, ErrPromptOnScreen) {
			t.Fatalf("err = %v, want a prompt refusal", err)
		}
		if f.Count("send-keys-text") != 1 || f.Count("enter") != 0 {
			t.Fatalf("want the text typed and no Enter: %v", f.CallsSnapshot())
		}
		if RefusedBeforeTyping(err) {
			t.Fatalf("the text is in the pane, yet %v counts as nothing typed", err)
		}
		if !TextMayBeInSession(err) {
			t.Fatalf("%v does not say the text may be in the session", err)
		}
	})
	t.Run("the key send fails", func(t *testing.T) {
		mgr, f, sess := newPromptSession(t, toolIdlePane)
		f.FailNextOp = map[string]int{"send-keys": 1, "send-keys-settle": 1}
		err := mgr.SendInput(sess.FullID, "carry on", "web")
		if err == nil || RefusedBeforeTyping(err) || !TextMayBeInSession(err) {
			t.Fatalf("err = %v, want an unsure send failure", err)
		}
	})
	t.Run("other errors", func(t *testing.T) {
		mgr, _, _ := newPromptSession(t, toolIdlePane)
		for _, err := range []error{nil, errors.New("exit status 1"), mgr.SendInput("nobody", "x", "automaton")} {
			if RefusedBeforeTyping(err) || TextMayBeInSession(err) {
				t.Fatalf("%v was read as a send outcome", err)
			}
		}
	})
}
