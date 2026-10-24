package main

import "testing"

// resetSSEStallState gives a test a brand-new shared fresh-tracker so
// scans from one test do not count as "previously seen" in another.
func resetSSEStallState() {
	sseFreshTracker = newSSEFreshTracker()
}

// TestSSEStallScan_B89Regression locks in the exact scrollback fragment
// observed in the B89 incident (2026-09-15): opencode's
// @ai-sdk/openai-compatible client aborted a qwen3.8:27b generation after a
// 6m9s silent "thinking" pause exceeded opencode's default 300s
// chunkTimeout, printing "SSE read timed out" inside the TUI box
// characters. The pattern list did not include this phrasing at the time,
// so the session stalled ~2h undetected until an operator manually killed
// it. This test fails if that exact line (or its casing) ever stops
// matching again.
func TestSSEStallScan_B89Regression(t *testing.T) {
	resetSSEStallState()
	scrollback := "┃ ┃ ┃    SSE read timed out ┃ ┃ ┃"
	m, ok := sseStallScan("sess-b89", "opencode", scrollback, sseStallSourceWaitLoop)
	if !ok {
		t.Fatalf("sseStallScan did not match observed B89 scrollback: %q", scrollback)
	}
	if m.Pattern != "SSE read timed out" {
		t.Errorf("matched pattern = %q, want %q", m.Pattern, "SSE read timed out")
	}
	if m.Backend != "opencode" || m.Source != sseStallSourceWaitLoop {
		t.Errorf("match backend/source = %q/%q, want opencode/%q", m.Backend, m.Source, sseStallSourceWaitLoop)
	}
}

func TestSSEStallScan_AllKnownPatterns(t *testing.T) {
	for _, pat := range autonomousSSEStallPatterns {
		t.Run(pat, func(t *testing.T) {
			resetSSEStallState()
			scrollback := "some opencode TUI noise\n" + pat + "\nmore noise"
			if m, ok := sseStallScan("sess-"+pat, "opencode-acp", scrollback, sseStallSourceWatchdog); !ok {
				t.Errorf("pattern %q did not match its own scrollback line", pat)
			} else if m.Pattern != pat {
				t.Errorf("matched pattern = %q, want %q", m.Pattern, pat)
			}
		})
	}
}

func TestSSEStallScan_CaseInsensitive(t *testing.T) {
	// B83/B85/B89 were all "a new phrasing wasn't in the list" bugs. Casing
	// drift specifically should never cause a fourth recurrence.
	resetSSEStallState()
	scrollback := "SSE READ TIMED OUT"
	m, ok := sseStallScan("sess-case", "opencode", scrollback, sseStallSourceWatchdog)
	if !ok {
		t.Fatalf("expected case-insensitive match for %q", scrollback)
	}
	if m.Pattern != "SSE read timed out" {
		t.Errorf("matched pattern = %q, want %q", m.Pattern, "SSE read timed out")
	}
}

func TestSSEStallScan_ErrorPrefixAccepted(t *testing.T) {
	resetSSEStallState()
	for _, sb := range []string{
		"Error: connection refused",
		"AI_APICallError: no such host",
	} {
		if m, ok := sseStallScan("sess-prefix", "opencode", sb, sseStallSourceWaitLoop); !ok {
			t.Errorf("expected match for provider-error line %q", sb)
		} else if m.Line != sb {
			t.Errorf("matched line = %q, want %q", m.Line, sb)
		}
	}
}

func TestSSEStallScan_NotOpencodeFamily(t *testing.T) {
	// A claude-code worker whose pane contains these phrases in a test it
	// wrote or printed must not be ended by this detector.
	resetSSEStallState()
	scrollback := "func TestDial(t *testing.T) {\n\twant connection refused\n}"
	if _, ok := sseStallScan("sess-claude", "claudecode", scrollback, sseStallSourceWaitLoop); ok {
		t.Error("matched a non-opencode backend family; detector must be family-gated")
	}
	for _, fam := range []string{"claudecode", "pi-prompt", "openwebui", "ollama", ""} {
		if sseStallAppliesTo(fam) {
			t.Errorf("sseStallAppliesTo(%q) = true, want false", fam)
		}
		if _, ok := sseStallScan("sess-"+fam, fam, "connection refused", sseStallSourceWatchdog); ok {
			t.Errorf("sseStallScan matched family %q, want it gated out", fam)
		}
	}
	for _, fam := range []string{"opencode", "opencode-acp", "opencode-prompt"} {
		if !sseStallAppliesTo(fam) {
			t.Errorf("sseStallAppliesTo(%q) = false, want true", fam)
		}
	}
}

func TestSSEStallScan_RejectsCodeAndToolContent(t *testing.T) {
	cases := []string{
		`"connection refused" appears when ollama is down`,        // quoted
		"t.Fatalf(\"want: connection refused\")",                  // test code
		"expected connection refused from the mock",               // assertion prose
		"err = doConnect(ctx)",                                    // no phrase but code shape
		"12\tconnection refused",                                  // leading line number
		"server.go:88:17: connection refused",                     // file:line:col
		"⎿ connection refused",                                    // tool-result marker
		"$ curl http://127.0.0.1:11434 → connection refused",      // prompt + prose
		"```\"SSE error\"```",                                     // fenced
		"the worker wrote connection refused into docs/README.md", // mid-line mention
	}
	resetSSEStallState()
	for _, sb := range cases {
		if m, ok := sseStallScan("sess-reject", "opencode", sb, sseStallSourceWaitLoop); ok {
			t.Errorf("unexpected match on %q (line %q pattern %q)", sb, m.Line, m.Pattern)
		}
	}
}

func TestSSEStallScan_FreshTextRule(t *testing.T) {
	resetSSEStallState()
	scrollbackBefore := "working on the task\n"
	if _, ok := sseStallScan("sess-fresh", "opencode", scrollbackBefore, sseStallSourceWaitLoop); ok {
		t.Fatal("matched unrelated pre-existing noise")
	}
	// The error line now appears: it is new relative to the previous scan.
	scrollbackAfter := scrollbackBefore + "SSE read timed out\n"
	m, ok := sseStallScan("sess-fresh", "opencode", scrollbackAfter, sseStallSourceWaitLoop)
	if !ok {
		t.Fatal("did not match the newly appeared error line")
	}
	if m.Line != "SSE read timed out" {
		t.Errorf("matched line = %q, want the error line", m.Line)
	}
	// The same scrollback scanned again: the line is no longer new, so a
	// pane that merely contains the old error must not keep tripping.
	if _, ok := sseStallScan("sess-fresh", "opencode", scrollbackAfter, sseStallSourceWatchdog); ok {
		t.Error("matched again on unchanged scrollback; fresh-text rule should have held it")
	}
	// Forget (as when the session finishes) resets the session's history.
	sseFreshTracker.forget("sess-fresh")
	if _, ok := sseStallScan("sess-fresh", "opencode", scrollbackAfter, sseStallSourceWatchdog); !ok {
		t.Error("expected a match again after forget reset the session's history")
	}
}
