// A session's start-up is over once it shows its main prompt or works, or its
// task is known to have arrived. The start poller stops there.

package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartupOver(t *testing.T) {
	recorded := func(name string) string {
		raw, err := os.ReadFile(filepath.Join("testdata", "delivery", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	cases := map[string]struct {
		pane string
		over bool
	}{
		"nothing drawn yet":          {"", false},
		"the typed launch line":      {"user@host:~$ cd '/p' && claude 'tag the new version'; echo 'DATAWATCH_COMPLETE: claude done'\n", false},
		"a shell whose prompt is ❯":  {"~/work\n❯ cd '/p' && claude 'tag the new version'\n", false},
		"recorded channels dialog":   {recorded("claude-2.1.331-dev-channels-dialog.txt"), false},
		"recorded idle prompt":       {recorded("claude-2.1.331-startup-idle.txt"), true},
		"recorded idle prompt, 330":  {recorded("claude-2.1.330-startup-idle-placeholder.txt"), true},
		"recorded unsent paste":      {recorded("claude-2.1.330-unsubmitted-paste.txt"), true},
		"recorded working":           {recorded("claude-2.1.331-working.txt"), true},
		"working with no input box":  {"> do it\n● Next I will tag the new version.\n✻ Working… (esc to interrupt)\n", true},
		"idle prompt with shortcuts": {"▐▛███▜▌  Claude Code\n\n❯ Try \"x\"\n  ? for shortcuts\n", true},
	}
	for label, d := range realDialogs(t) {
		cases["dialog: "+label] = struct {
			pane string
			over bool
		}{d.pane, false}
	}
	for label, tc := range cases {
		mgr, fake := newTestManagerWithFake(t)
		sess := newStartupDialogSession(t, mgr, "claude-code", false)
		fake.Pane[sess.TmuxSession] = tc.pane
		if got := mgr.StartupOver(sess.FullID); got != tc.over {
			t.Errorf("%s: over = %v, want %v", label, got, tc.over)
		}
	}
}

func TestStartupOverByTaskAndState(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", false)
	fake.Pane[sess.TmuxSession] = ""
	// A claim is made before the text is seen to arrive; a dialog may still be up.
	if !mgr.ClaimTaskDelivery(sess.FullID) || mgr.StartupOver(sess.FullID) {
		t.Fatal("an unproven delivery must not end start-up")
	}
	mgr.MarkTaskDelivered(sess.FullID)
	if !mgr.StartupOver(sess.FullID) {
		t.Fatal("a delivered task ends start-up")
	}
	ended := addNamedHolder(t, mgr, fake, "aa09", "gone", StateFailed, "")
	if !mgr.StartupOver(ended.FullID) || !mgr.StartupOver("no-such-session") {
		t.Fatal("an ended or missing session has no start-up left")
	}
}

// The record of an answered screen is dropped once the live screen no longer
// shows that dialog, even if its words are still in the output.
func TestAnAnsweredScreenIsGoneThoughItsWordsRemain(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", false)
	if err := mgr.AnswerStartupScreen(sess.FullID, StartupDialogConfirmUnsafe, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	fake.Pane[sess.TmuxSession] = "● the dialog asked: install the update to your settings?\n  Enter to confirm · Esc to cancel" + inputBox
	mgr.startupScreenLeft(sess, StartupDialogConfirmUnsafe)
	if err := mgr.AnswerStartupScreen(sess.FullID, StartupDialogConfirmUnsafe, func() error { return nil }); err != nil {
		t.Fatalf("a later dialog of this kind would never be answered: %v", err)
	}
}
