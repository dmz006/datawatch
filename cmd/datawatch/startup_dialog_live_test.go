// The daemon's start-up responders (the prompt line one, the state change one
// and the start poller) send a key only to a dialog on the live screen.

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

const liveRule = "────────────────────────────────────────────────────────────────────────"

func liveManager(t *testing.T, pane string) (*session.Manager, *session.FakeTmux) {
	t.Helper()
	m, fake := newStartupWiringManager(t, "idle-prompt.txt")
	fake.Pane[startupTestTmux] = pane
	return m, fake
}

func liveKeys(f *session.FakeTmux) (trust, text, esc int, last string) {
	if c := f.LastCall("send-keys"); len(c.Args) > 0 {
		last = c.Args[0]
	}
	return f.Count("SendTrustPromptAccept"), f.Count("send-keys") + f.Count("send-keys-settle") + f.Count("enter"),
		f.Count("send-keys-literal"), last
}

func TestLiveResponderSendsNothingForWordsInOutput(t *testing.T) {
	box := "\n" + liveRule + "\n❯ \n" + liveRule + "\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
	for _, phrase := range []string{
		"Quick safety check", "trust this folder", "Yes, I trust this folder",
		"2. Yes, I accept  1. No, exit", "WARNING: Loading development channels",
		"I am using this for local development", "New MCP server found", "use this MCP server",
		"Continue? Enter to confirm · Esc to cancel", "Update your settings? Enter to confirm · Esc to cancel",
		"Update available", "Update now?", "I will tag the new version and push it.",
	} {
		for where, pane := range map[string]string{
			"working":           "> do it\n● " + phrase + "\n✻ Working… (esc to interrupt)\n",
			"above the box":     "● " + phrase + box,
			"a task in the box": liveRule + "\n❯ please look at: " + phrase + "\n" + liveRule + "\n  ⏵⏵ bypass permissions on\n",
		} {
			m, fake := liveManager(t, pane)
			answered := answerLiveStartupDialog(m, startupTestFullID)
			if trust, text, esc, last := liveKeys(fake); answered || trust+text+esc != 0 {
				t.Errorf("%q, %s: trust=%d typed=%d (%q) esc=%d", phrase, where, trust, text, last, esc)
			}
		}
	}
}

func TestLiveResponderAnswersRealDialogsOnce(t *testing.T) {
	foot := "\n\n  Enter to confirm · Esc to cancel"
	for label, tc := range map[string]struct {
		pane             string
		trust, text, esc int
		last             string
	}{
		"trust":    {liveRule + "\nQuick safety check\n\n❯ 1. No, exit\n  2. Yes, I trust this folder" + foot, 1, 0, 0, ""},
		"channels": {liveRule + "\n  WARNING: Loading development channels\n\n  ❯ 1. I am using this for local development\n    2. Exit" + foot, 0, 1, 0, "\n"},
		"bypass":   {liveRule + "\n  Bypass Permissions mode\n\n  ❯ 1. No, exit\n    2. Yes, I accept" + foot, 0, 1, 0, "2\n"},
		"mcp":      {liveRule + "\n  New MCP server found\n\n  ❯ 1. Use this MCP server\n    2. Skip" + foot, 0, 1, 0, "1\n"},
		"continue": {liveRule + "\n  Proceed with these channels active?" + foot, 0, 1, 0, "\n"},
		"settings": {liveRule + "\n Settings Error\n ❯ 1. Fix with Claude\n   2. Exit and fix manually\n   3. Continue without these settings" + foot, 0, 0, 1, ""},
	} {
		m, fake := liveManager(t, tc.pane)
		fake.NoEcho = true
		if !answerLiveStartupDialog(m, startupTestFullID) {
			t.Errorf("%s: not answered", label)
		}
		trust, text, esc, last := liveKeys(fake)
		if trust != tc.trust || text != tc.text || esc != tc.esc || (tc.last != "" && last != tc.last) {
			t.Errorf("%s: trust=%d typed=%d (%q) esc=%d, want %d %d (%q) %d", label, trust, text, last, esc, tc.trust, tc.text, tc.last, tc.esc)
		}
	}
}

// Every live responder in the daemon goes through the screen check.
func TestDaemonRespondersCheckTheLiveScreen(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "respondStartupDialog(mgr") {
			t.Errorf("main.go:%d answers without checking the live screen: %s", i+1, strings.TrimSpace(line))
		}
	}
}
