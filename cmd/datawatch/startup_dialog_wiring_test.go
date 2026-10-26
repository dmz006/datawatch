// Startup dialog wiring — feeds every recorded fixture from
// internal/session/testdata/startup/ through the handler decision
// function (respondStartupDialog) with a FakeTmux pane source, and asserts
// the exact keys sent (or none) plus that Esc is sent exactly once for the
// unsafe dialogs (confirm-unsafe, update-prompt).

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

const startupTestFullID = "testhost-startup-1"
const startupTestTmux = "cs-startup-1"

// newStartupWiringManager builds a Manager whose tmux layer is a FakeTmux,
// with one claude-code session in waiting_input ready to receive input.
// The pane content is the fixture under test.
func newStartupWiringManager(t *testing.T, fixture string) (*session.Manager, *session.FakeTmux) {
	t.Helper()
	m, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTestTemp(m)
	fake := m.WithFakeTmux()
	_ = m.SaveSession(&session.Session{
		ID:            "startup-1",
		FullID:        startupTestFullID,
		TmuxSession:   startupTestTmux,
		BackendFamily: "claude-code",
		State:         session.StateWaitingInput,
		UpdatedAt:     time.Now(),
	})
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "session", "testdata", "startup", fixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	fake.Pane[startupTestTmux] = string(data)
	return m, fake
}

// classifyFixturesAndAssertSends runs every startup fixture through the
// classifier and the handler decision function, asserting:
//
//   - ActiveStartupDialog returns the README-documented kind;
//   - for Enter-type dialogs the exact rendered text was sent via
//     SendInput (recorded as a single one-shot SendKeys call);
//   - for the trust-folder dialog the arrow-key path (SendTrustPromptAccept)
//     was the only send;
//   - for the retry (unsafe) dialogs exactly ONE Esc keystroke went through
//     the tmux layer and nothing else;
//   - for unknown-confirm and the plain idle prompt nothing was sent.
func classifyFixturesAndAssertSends(t *testing.T) {
	cases := []struct {
		fixture      string
		kind         session.StartupKind
		wantSendKeys string // text the SendKeys path should carry; "" → none
		wantTrust    bool   // arrow-key accept path
		wantEsc      bool   // single Esc decline
	}{
		{"trust-folder.txt", session.StartupDialogTrustFolder, "", true, false},
		{"bypass-accept.txt", session.StartupDialogBypassAccept, "2\n", false, false},
		{"dev-channels.txt", session.StartupDialogDevChannels, "\n", false, false},
		{"mcp-trust.txt", session.StartupDialogMCPTrust, "1\n", false, false},
		{"confirm-continue.txt", session.StartupDialogConfirmContinue, "\n", false, false},
		{"confirm-unsafe.txt", session.StartupDialogConfirmUnsafe, "", false, true},
		{"update-prompt.txt", session.StartupDialogUpdatePrompt, "", false, true},
		{"unknown-confirm.txt", session.StartupDialogUnknownConfirm, "", false, false},
		{"idle-prompt.txt", session.StartupDialogNone, "", false, false},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			m, fake := newStartupWiringManager(t, c.fixture)

			// The classifier the manager delegates to.
			got := m.ActiveStartupDialog(startupTestFullID)
			if got.Kind != c.kind {
				t.Fatalf("ActiveStartupDialog kind = %q, want %q", got.Kind, c.kind)
			}

			responded := respondStartupDialog(m, startupTestFullID)

			if c.wantEsc && !responded {
				t.Fatalf("unsafe dialog %s: decision function reported not answered", c.kind)
			}
			if !c.wantEsc && !c.wantTrust && c.wantSendKeys != "" && !responded {
				t.Fatalf("%s: decision function reported not answered", c.kind)
			}

			// Trust-folder: arrow-key path only.
			if c.wantTrust {
				if got := fake.Count("SendTrustPromptAccept"); got != 1 {
					t.Fatalf("trust dialog: SendTrustPromptAccept calls = %d, want 1 (calls: %v)", got, fake.Calls)
				}
			} else if got := fake.Count("SendTrustPromptAccept"); got != 0 {
				t.Fatalf("non-trust dialog %s: SendTrustPromptAccept calls = %d, want 0", c.kind, got)
			}

			// Enter-type sends: exactly one SendKeys call, with the exact text.
			if c.wantSendKeys != "" {
				if got := fake.Count("send-keys"); got != 1 {
					t.Fatalf("%s: send-keys calls = %d, want 1 (calls: %v)", c.kind, got, fake.Calls)
				}
				if call := fake.LastCall("send-keys"); call.Args[0] != c.wantSendKeys {
					t.Fatalf("%s: send-keys text = %q, want %q", c.kind, call.Args[0], c.wantSendKeys)
				}
			} else if got := fake.Count("send-keys"); got != 0 {
				call := fake.LastCall("send-keys")
				if len(call.Args) > 0 {
					t.Fatalf("%s: expected no SendKeys send, got text %q (calls: %v)", c.kind, call.Args[0], fake.Calls)
				}
			}

			// Unsafe dialogs: exactly one Esc through the tmux layer.
			escCalls := fake.Count("send-keys-literal")
			if c.wantEsc {
				if escCalls != 1 {
					t.Fatalf("%s: Esc (send-keys-literal) calls = %d, want exactly 1 (calls: %v)", c.kind, escCalls, fake.Calls)
				}
				call := fake.LastCall("send-keys-literal")
				if len(call.Args) != 1 || call.Args[0] != "\x1b" {
					t.Fatalf("%s: Esc payload = %q, want exactly byte 0x1b", c.kind, call.Args)
				}
			} else if escCalls != 0 {
				t.Fatalf("%s: Esc (send-keys-literal) calls = %d, want 0", c.kind, escCalls)
			}

			// Nothing else should touch the pane (no session lifecycle ops
			// from the decision function).
			for _, op := range []string{"new", "kill", "resize", "send-keys-settle"} {
				if got := fake.Count(op); got != 0 {
					t.Fatalf("%s: unexpected %s calls = %d", c.kind, op, got)
				}
			}
		})
	}
}

func TestStartupDialogWiring(t *testing.T) {
	classifyFixturesAndAssertSends(t)
}

// TestStartupDialogWiring_NoRetryUnsafe asserts the safety guarantee: with
// the unsafe dialog on the pane a single decision pass must never accept
// (no Enter-type keys, no trust arrow keys) and must send exactly one Esc —
// the handler loop then breaks, so the daemon never retries the send; the
// start-failure budget is the only re-try owner.
func TestStartupDialogWiring_NoRetryUnsafe(t *testing.T) {
	for _, fixture := range []string{"confirm-unsafe.txt", "update-prompt.txt"} {
		t.Run(fixture, func(t *testing.T) {
			m, fake := newStartupWiringManager(t, fixture)
			// The handler loop runs until the decision function reports an
			// answer — for a declined dialog that is exactly one pass.
			if !respondStartupDialog(m, startupTestFullID) {
				t.Fatalf("declined dialog should report answered")
			}
			if got := fake.Count("send-keys-literal"); got != 1 {
				t.Fatalf("Esc calls = %d, want exactly 1 (calls: %v)", got, fake.Calls)
			}
			if got := fake.Count("send-keys"); got != 0 {
				t.Fatalf("accepted-keys calls = %d, want 0 — unsafe dialog must never be accepted", got)
			}
			if got := fake.Count("SendTrustPromptAccept"); got != 0 {
				t.Fatalf("trust-accept calls = %d, want 0", got)
			}
		})
	}
}

// TestStartupDialogWiring_ClassifierGate checks the line-level gate
// (claudeDisclaimerResponse) against the same fixtures: every fixture the
// classifier maps to a non-"none" kind must open the gate so the pane-level
// handler runs, and the plain idle prompt must not.
func TestStartupDialogWiring_ClassifierGate(t *testing.T) {
	fixtures := []string{
		"trust-folder.txt", "bypass-accept.txt", "dev-channels.txt",
		"mcp-trust.txt", "confirm-continue.txt", "confirm-unsafe.txt",
		"update-prompt.txt", "unknown-confirm.txt",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			m, fake := newStartupWiringManager(t, fixture)
			got := m.ActiveStartupDialog(startupTestFullID)
			if got.Kind == session.StartupDialogNone {
				t.Fatalf("%s: classifier says none — gate would not run the handler", fixture)
			}
			if tag := claudeDisclaimerResponse(string(fake.Pane[startupTestTmux])); tag == "" {
				t.Fatalf("%s: gate returned empty tag — handler would be skipped", fixture)
			}
		})
	}
	t.Run("idle-prompt.txt", func(t *testing.T) {
		m, fake := newStartupWiringManager(t, "idle-prompt.txt")
		got := m.ActiveStartupDialog(startupTestFullID)
		if got.Kind != session.StartupDialogNone {
			t.Fatalf("idle prompt: kind = %q, want none", got.Kind)
		}
		if tag := claudeDisclaimerResponse(string(fake.Pane[startupTestTmux])); tag != "" {
			t.Fatalf("idle prompt: gate tag = %q, want empty", tag)
		}
	})
}
