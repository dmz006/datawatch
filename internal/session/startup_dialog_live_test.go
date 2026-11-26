// A key is sent to a start-up dialog only when the dialog itself is at the
// bottom of the live screen. Its words anywhere else on a screen, in a
// session's output or in a task, send nothing.

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rule = "────────────────────────────────────────────────────────────────────────"

const liveFooter = "\n\n  Enter to confirm · Esc to cancel"

// realDialogs are dialog screens as Claude Code draws them, each with the keys
// that answer it. The development channels one is a recorded 2.1.331 screen.
// The bypass and MCP ones are made up in the same shape: no recording of them
// is in the repository.
func realDialogs(t testing.TB) map[string]struct {
	pane string
	kind StartupKind
	keys []string
} {
	read := func(parts ...string) string {
		raw, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(raw)
	}
	both := strings.SplitN(read("claude_trust_dialog.txt"), "; --- channels-consent pane ---", 2)
	type d = struct {
		pane string
		kind StartupKind
		keys []string
	}
	return map[string]d{
		"trust folder":          {both[0], StartupDialogTrustFolder, []string{"Down", "Enter"}},
		"channels":              {both[1], StartupDialogDevChannels, []string{"Enter"}},
		"channels, recorded":    {read("delivery", "claude-2.1.331-dev-channels-dialog.txt"), StartupDialogDevChannels, []string{"Enter"}},
		"bypass":                {rule + "\n  WARNING: Claude Code running in Bypass Permissions mode\n\n  ❯ 1. No, exit\n    2. Yes, I accept" + liveFooter, StartupDialogBypassAccept, []string{"2", "Enter"}},
		"mcp server":            {rule + "\n  New MCP server found in .mcp.json: files\n\n  ❯ 1. Use this and all future MCP servers in this project\n    2. Use this MCP server\n    3. Continue without using this MCP server" + liveFooter, StartupDialogMCPTrust, []string{"1", "Enter"}},
		"confirm to continue":   {read("startup", "confirm-continue.txt"), StartupDialogConfirmContinue, []string{"Enter"}},
		"confirm to be refused": {read("startup", "confirm-unsafe.txt"), StartupDialogConfirmUnsafe, []string{"Esc"}},
		"settings error": {rule + "\n Settings Error\n\n /made-up/project/.claude/settings.json\n  └ Invalid or malformed JSON\n\n ❯ 1. Fix with Claude\n   2. Exit and fix manually\n   3. Continue without these settings" + liveFooter,
			StartupDialogConfirmUnsafe, []string{"Esc"}},
		// Notes from a live run of Claude Code 2.1.306 write the trust
		// options without numbers, with either one preselected.
		"trust folder, no numbers":          {rule + "\n Quick safety check: Is this a project you trust?\n\n ❯ Yes, I trust this folder\n   No, exit" + liveFooter, StartupDialogTrustFolder, []string{"Down", "Enter"}},
		"trust folder, no numbers, on exit": {rule + "\n Quick safety check: Is this a project you trust?\n\n ❯ No, exit\n   Yes, I trust this folder" + liveFooter, StartupDialogTrustFolder, []string{"Down", "Enter"}},
		// Output above a dialog that names another dialog does not change
		// which one is at the bottom.
		"words of another above": {"the task mentions trust this folder and a new version\n" + both[1], StartupDialogDevChannels, []string{"Enter"}},
		"boxed footer":           {"╭" + rule + "╮\n│ New MCP server found\n│ ❯ 1. Use this MCP server\n│   2. Skip\n│ Enter to confirm · Esc to cancel │\n╰" + rule + "╯\n\n", StartupDialogMCPTrust, []string{"1", "Enter"}},
	}
}

// dialogPhrases holds the words that name each kind of dialog.
var dialogPhrases = map[string]string{
	"trust, heading":      "Quick safety check",
	"trust, folder":       "trust this folder",
	"trust, option":       "Yes, I trust this folder",
	"bypass":              "2. Yes, I accept  1. No, exit",
	"channels, warning":   "WARNING: Loading development channels",
	"channels, option":    "I am using this for local development",
	"mcp, heading":        "New MCP server found",
	"mcp, question":       "Do you want to use this MCP server?",
	"confirm to continue": "Continue with these channels? Enter to confirm · Esc to cancel",
	"confirm to refuse":   "Install the update to your settings? Enter to confirm · Esc to cancel",
	"update, available":   "Update available",
	"update, now":         "Update now?",
	"update, new version": "Next I will tag the new version and push it.",
}

const inputBox = "\n" + rule + "\n❯ \n" + rule + "\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

// ordinaryScreens puts text where a session's output or a task can be.
func ordinaryScreens(text string) map[string]string {
	quoted := strings.ReplaceAll(text, "\n", "\n  ")
	return map[string]string{
		"output while working":    "● " + quoted + "\n✻ Working… (esc to interrupt)\n",
		"output above the box":    "● " + quoted + inputBox,
		"working above the box":   "● " + quoted + "\n✻ Cascading… (8m 10s)" + inputBox,
		"a sent task":             "> " + quoted + "\n● reading" + inputBox,
		"a task still in the box": rule + "\n❯ " + quoted + "\n" + rule + "\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n",
		// The input box's own closing rule is the last thing drawn.
		"a task in the box, nothing below it": rule + "\n❯ " + quoted + "\n" + rule + "\n",
		"output above the box, nothing below": "● " + quoted + "\n" + rule + "\n❯ \n" + rule + "\n",
		// An input box drawn as a rounded box, with nothing below it.
		"a task in a rounded box":    "╭" + rule + "╮\n│ > " + strings.ReplaceAll(text, "\n", "\n│   ") + "\n╰" + rule + "╯\n",
		"a task in a rounded box, ❯": "╭" + rule + "╮\n│ ❯ please read this\n│   " + strings.ReplaceAll(text, "\n", "\n│   ") + "\n╰" + rule + "╯\n",
		"the typed launch line":      "user@host:~$ cd '/p' && claude 'do this: " + text + "'; echo 'DATAWATCH_COMPLETE: claude done'\n",
		"output at the very end":     "● " + quoted + "\n",
	}
}

// ordinaryTexts is every phrase, and every real dialog copied whole, as a
// pasted task would hold it.
func ordinaryTexts(t testing.TB) map[string]string {
	out := map[string]string{}
	for k, v := range dialogPhrases {
		out[k] = v
	}
	for k, d := range realDialogs(t) {
		out["a copy of the "+k+" screen"] = "please explain this:\n" + strings.TrimRight(d.pane, " \n")
	}
	return out
}

// copyAtEnd is the one place where a whole copy of a dialog cannot be told
// from the dialog: raw output that ends with it, nothing drawn below. Claude
// Code always draws its input box or spinner below output.
func copyAtEnd(text, screen string) bool {
	return strings.HasPrefix(text, "a copy of the ") && screen == "output at the very end"
}

func TestRealDialogScreensAreLive(t *testing.T) {
	for label, d := range realDialogs(t) {
		got, ok := liveStartupDialog(d.pane)
		if !ok || got.Kind != d.kind || strings.Join(got.Keys, " ") != strings.Join(d.keys, " ") {
			t.Errorf("%s: live=%v kind=%q keys=%v, want %q %v", label, ok, got.Kind, got.Keys, d.kind, d.keys)
		}
	}
}

func TestDialogWordsElsewhereAreNotADialog(t *testing.T) {
	for what, text := range ordinaryTexts(t) {
		for where, pane := range ordinaryScreens(text) {
			if copyAtEnd(what, where) {
				continue
			}
			if got, ok := liveStartupDialog(pane); ok && len(got.Keys) > 0 {
				t.Errorf("%s, %s: read as %q, keys %v", what, where, got.Kind, got.Keys)
			}
		}
	}
}

// A screen the agent left behind is not waiting for a key.
func TestADialogTheAgentLeftIsNotLive(t *testing.T) {
	for label, d := range realDialogs(t) {
		pane := strings.TrimRight(d.pane, " \n") + "\nDATAWATCH_COMPLETE: claude done\nuser@host:~$ \n"
		if got, ok := liveStartupDialog(pane); ok && len(got.Keys) > 0 {
			t.Errorf("%s: read as %q", label, got.Kind)
		}
	}
}

// No Esc without a dialog footer: not for the recorded words of an update
// notice either.
func TestNoEscWithoutAFooter(t *testing.T) {
	for _, pane := range []string{
		rule + "\n  Update available: 2.1.9\n\n  Update now?\n\n  ❯\n",
		rule + "\n  New version ready.\n\n  ❯ 1. Update now\n    2. Later\n",
	} {
		if got, ok := liveStartupDialog(pane); ok && len(got.Keys) > 0 {
			t.Errorf("%q read as %q, keys %v", pane, got.Kind, got.Keys)
		}
	}
}

// The one-shot responder reads both the pane and single log lines. A line is
// only a reason to look: the live screen decides.
func TestOneShotResponderIgnoresDialogWordsInOutput(t *testing.T) {
	texts := ordinaryTexts(t)
	for what, text := range texts {
		for where, pane := range ordinaryScreens(text) {
			if copyAtEnd(what, where) {
				continue
			}
			mgr, fake := newTestManagerWithFake(t)
			sess := newStartupDialogSession(t, mgr, "claude-code", true)
			fake.Pane[sess.TmuxSession] = pane
			mgr.dismissClaudeStartupDialog(sess, pane)
			for _, line := range strings.Split(text, "\n") {
				mgr.dismissClaudeStartupDialog(sess, line)
			}
			if calls := typedCalls(fake); len(calls) != 0 || trustSends(fake) != 0 {
				t.Errorf("%s, %s: typed %+v (trust accepts %d)", what, where, calls, trustSends(fake))
			}
		}
	}
}

// A log line of a dialog that is on the screen still gets it answered, once.
func TestOneShotResponderAnswersTheDialogOnScreen(t *testing.T) {
	for label, want := range map[string][2]int{"trust folder": {1, 0}, "channels": {0, 1}, "channels, recorded": {0, 1}} {
		d := realDialogs(t)[label]
		mgr, fake := newTestManagerWithFake(t)
		sess := newStartupDialogSession(t, mgr, "claude-code", true)
		fake.Pane[sess.TmuxSession] = d.pane
		fake.NoEcho = true
		line := "Quick safety check ... I am using this for local development"
		if want[0] == 0 {
			line = " ❯ 1. I am using this for local development"
		}
		mgr.dismissClaudeStartupDialog(sess, line)
		mgr.dismissClaudeStartupDialog(sess, d.pane)
		if got := [2]int{trustSends(fake), channelSends(fake)}; got != want {
			t.Errorf("%s: trust, channels accepts = %v, want %v", label, got, want)
		}
	}
}
