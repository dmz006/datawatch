// Nothing types an answer to a permission or safety prompt on its own.
//
// Every test here drives the daemon's own senders against a pane that shows
// an approval menu and asserts that no keystroke reaches the pane.

package session

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// approvalPane is a tool-approval menu with "Yes" preselected: any Enter
// sent to it approves the command.
const approvalPane = ` Bash command

   rm -rf ./build
   Remove build output

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands
   3. No, and tell Claude what to do differently (esc)
`

// safetyFooterPane is a safety confirmation that carries the same footer as
// a startup dialog, so the startup auto-accept can mistake it for one.
const safetyFooterPane = ` This command was flagged by a safety check. Only a person may approve it.

 Do you want to proceed?
 ❯ 1. Yes
   2. No

 Enter to confirm · Esc to cancel
`

func newPromptSession(t *testing.T, pane string) (*Manager, *FakeTmux, *Session) {
	t.Helper()
	mgr := newTestManager(t)
	f := mgr.WithFakeTmux()
	sess := &Session{
		ID: "ab12", FullID: "testhost-ab12", Hostname: "testhost",
		TmuxSession: "cs-ab12", BackendFamily: "claude-code",
		State: StateWaitingInput, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	f.Pane[sess.TmuxSession] = pane
	return mgr, f, sess
}

func typedCalls(f *FakeTmux) []FakeTmuxCall {
	var out []FakeTmuxCall
	for _, c := range f.CallsSnapshot() {
		switch c.Op {
		case "send-keys", "send-keys-settle", "send-keys-literal", "enter", "SendTrustPromptAccept":
			out = append(out, c)
		}
	}
	return out
}

func TestUpgradeDisablesSeededInputTypingFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	old := `[{"id":"2ddd","pattern":"Do you want to proceed\\?","action":"schedule","value":"yes","enabled":true,"seeded":true},
{"id":"aaaa","pattern":"quota exceeded","action":"alert","value":"x","enabled":true,"seeded":true}]`
	if err := writeFileForTest(path, old); err != nil {
		t.Fatal(err)
	}
	fs, err := NewFilterStore(path)
	if err != nil {
		t.Fatal(err)
	}
	check := func(fs *FilterStore, when string) {
		t.Helper()
		for _, f := range fs.List() {
			if f.ID == "2ddd" && f.Enabled {
				t.Errorf("%s: seeded input-typing filter 2ddd is still enabled", when)
			}
			if f.ID == "aaaa" && !f.Enabled {
				t.Errorf("%s: seeded alert filter was disabled", when)
			}
		}
	}
	check(fs, "after upgrade")
	if err := fs.SetEnabled("2ddd", true); err == nil {
		t.Error("re-enabling the seeded input-typing filter must be refused")
	}
	again, err := NewFilterStore(path)
	if err != nil {
		t.Fatal(err)
	}
	check(again, "after a second start")
}

func TestDaemonSendersNeverAnswerApprovalPrompt(t *testing.T) {
	sources := []string{
		"filter", "schedule", "exit-hook", "supervisor-wake", "discussion-sub", "mcp-reply",
		"lifecycle-sub", "idle-nudge", "task-redelivery", "loop-rescue", "automaton",
		"channel-task", "questions-answer", "auto-accept-disclaimer",
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			mgr, f, sess := newPromptSession(t, approvalPane)
			err := mgr.SendInput(sess.FullID, "yes", src)
			if calls := typedCalls(f); len(calls) != 0 {
				t.Fatalf("source %q typed into an approval prompt: %+v", src, calls)
			}
			if err == nil {
				t.Errorf("source %q: want a refusal error", src)
			}
		})
	}
}

// A person answering the prompt is the point of the product: it still works.
func TestPersonCanStillAnswerApprovalPrompt(t *testing.T) {
	for _, src := range []string{"web", "api", "cli", "mcp", "signal"} {
		mgr, f, sess := newPromptSession(t, approvalPane)
		if err := mgr.SendInput(sess.FullID, "1", src); err != nil {
			t.Fatalf("source %q: %v", src, err)
		}
		if len(typedCalls(f)) == 0 {
			t.Fatalf("source %q: nothing typed", src)
		}
	}
}

func TestStartupClassifierNeverAcceptsApprovalPrompt(t *testing.T) {
	for name, pane := range map[string]string{"menu": approvalPane, "footer": safetyFooterPane} {
		a := ClassifyStartupDialog(pane)
		if len(a.Keys) != 0 {
			t.Errorf("%s: classifier would send %v (kind %s)", name, a.Keys, a.Kind)
		}
	}
}

// Trust wording that is merely on screen must not make the one-shot dialog
// dismissal press keys on an approval menu.
func TestOneShotDismissNeverTouchesApprovalPrompt(t *testing.T) {
	pane := " notes: the Quick safety check dialog\n\n" + approvalPane
	mgr, f, sess := newPromptSession(t, pane)
	sess.OneShot = true
	mgr.dismissClaudeStartupDialog(sess, pane)
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("dismissal typed into an approval prompt: %+v", calls)
	}
}

// Whatever a filter types is marked, so it never reads as the operator.
func TestFilterTypedTextIsMarked(t *testing.T) {
	mgr, f, sess := newPromptSession(t, "❯ \n")
	_ = mgr.SendInput(sess.FullID, "yes", "filter")
	for _, c := range typedCalls(f) {
		if len(c.Args) > 0 && strings.Contains(c.Args[0], "yes") && !strings.Contains(c.Args[0], "not the operator") {
			t.Fatalf("filter typed unmarked text %q", c.Args[0])
		}
	}
}

// A submit retry must never press Enter on an approval menu that appeared
// after the first Enter.
func TestPasteRetryNeverEntersApprovalPrompt(t *testing.T) {
	mgr := newTestManager(t)
	f := mgr.WithFakeTmux()
	f.Pane["cs-r"] = "❯ [Pasted text #1 +3 lines]\n\n" + approvalPane
	if err := confirmPromptSubmitted(f, "cs-r", func() bool { return true }, time.Millisecond, time.Millisecond); err != nil {
		t.Fatalf("confirmPromptSubmitted: %v", err)
	}
	if calls := typedCalls(f); len(calls) != 0 {
		t.Fatalf("submit confirm pressed keys on an approval prompt: %+v", calls)
	}
}

// A destructive confirmation worded unlike the known approval prompts still
// carries the startup footer; the startup accept must not press Enter on it.
func TestStartupClassifierHoldsBackDestructiveConfirmations(t *testing.T) {
	for _, q := range []string{
		"This will permanently delete 40 files. Continue?",
		"Removal of the build directory was flagged. Proceed?",
		"This is a destructive operation. Do you accept?",
		"Allow the dangerous command to continue?",
	} {
		pane := " " + q + "\n\n   (Enter to confirm)   (Esc to cancel)\n"
		for _, k := range ClassifyStartupDialog(pane).Keys {
			if k == "Enter" {
				t.Errorf("%q: startup accept would press Enter", q)
			}
		}
	}
}

// scrolledPane shows an old view while the live screen holds an approval menu,
// as when someone has scrolled the pane up.
type scrolledPane struct {
	*FakeTmux
	live string
}

func (s scrolledPane) CapturePaneLiveTail(string) (string, error) { return s.live, nil }

func TestSubmitVetoReadsTheLiveScreen(t *testing.T) {
	f := NewFakeTmux()
	f.Pane["s"] = "older scrolled-back output"
	err := submitUnlessVetoed(scrolledPane{FakeTmux: f, live: approvalPane}, "s", "x", paneShowsApprovalMenu)
	if err == nil || f.Count("enter") != 0 {
		t.Fatalf("err=%v enters=%d: the veto must read the live screen", err, f.Count("enter"))
	}
}
