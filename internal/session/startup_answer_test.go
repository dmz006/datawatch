package session

import (
	"errors"
	"testing"
	"time"
)

// Log lines without the dialog's words do not prove the screen has gone: only
// the live pane does.
func TestLogLinesDoNotReopenAScreenStillUp(t *testing.T) {
	trustPane, _ := loadStartupDialogPanes(t)
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", true)
	fake.Pane[sess.TmuxSession] = trustPane
	mgr.dismissClaudeStartupDialog(sess, trustPane)
	mgr.dismissClaudeStartupDialog(sess, "an unrelated log line")
	time.Sleep(claudeDialogConfirmWindow + 50*time.Millisecond)
	mgr.dismissClaudeStartupDialog(sess, "another log line") // confirms "gone" from lines alone
	mgr.dismissClaudeStartupDialog(sess, trustPane)
	if n := trustSends(fake); n != 1 {
		t.Fatalf("accepts = %d, want 1", n)
	}
}

func TestStartupAnswerRules(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", false)
	trustPane, channelsPane := loadStartupDialogPanes(t)
	now := time.Now()
	mgr.clock = func() time.Time { return now }
	sent := 0
	answer := func(kind StartupKind) bool {
		err := mgr.AnswerStartupScreen(sess.FullID, kind, func() error { sent++; return nil })
		if err != nil && !errors.Is(err, ErrStartupScreenAnswered) {
			t.Fatal(err)
		}
		return err == nil
	}
	if !answer(StartupDialogTrustFolder) || answer(StartupDialogTrustFolder) {
		t.Fatal("the first answer is sent and the second is not")
	}
	if !answer(StartupDialogDevChannels) {
		t.Fatal("the next, different screen is answered")
	}
	// The same screen long after: the keys were lost.
	now = now.Add(startupAnswerLostAfter - time.Millisecond)
	if answer(StartupDialogTrustFolder) {
		t.Fatal("sent again too soon")
	}
	now = now.Add(2 * time.Millisecond)
	if !answer(StartupDialogTrustFolder) {
		t.Fatal("a screen still up after the limit may be answered again")
	}
	// A declined screen is never declined twice while it stays up.
	if !answer(StartupDialogConfirmUnsafe) {
		t.Fatal("decline not sent")
	}
	now = now.Add(time.Hour)
	if answer(StartupDialogConfirmUnsafe) {
		t.Fatal("declined twice")
	}
	// Once the live pane shows it left, a later one of the kind is new.
	fake.Pane[sess.TmuxSession] = trustPane
	if !answer(StartupDialogTrustFolder) {
		t.Fatal("setup")
	}
	mgr.startupScreenLeft(sess, StartupDialogTrustFolder) // still up: kept
	if answer(StartupDialogTrustFolder) {
		t.Fatal("reopened while still on the pane")
	}
	fake.Pane[sess.TmuxSession] = channelsPane
	mgr.startupScreenLeft(sess, StartupDialogTrustFolder)
	if !answer(StartupDialogTrustFolder) {
		t.Fatal("a screen that left and came back is answered")
	}
	// A failed send leaves the screen unanswered.
	boom := errors.New("tmux failed")
	if err := mgr.AnswerStartupScreen(sess.FullID, StartupDialogMCPTrust, func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if !answer(StartupDialogMCPTrust) {
		t.Fatal("not retried after a failed send")
	}
}
