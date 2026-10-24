package main

import (
	"strings"
	"testing"
)

// resetSSEStallState gives a test a brand-new shared fresh-tracker so
// scans from one test do not count as "previously seen" in another.
func resetSSEStallState() {
	sseFreshTracker = newSSEFreshTracker()
}

// phraseTestRunScrollback is what a healthy non-opencode worker's pane can
// legitimately look like while a test run it spawned prints provider-error
// phrasing: a Go test output line, a t.Fatalf source line, diff lines, a
// phrase inside a fenced code block, and a tool-result line. Under the old
// whole-scrollback substring search this scrollback would have ended the
// session and retried its task; the family gate plus the line matcher must
// keep the detector away from all of it.
var phraseTestRunScrollback = "=== RUN   TestDial\n" +
	"    dial_test.go:42: got: connection refused\n" +
	"--- FAIL: TestDial (0.01s)\n" +
	"func TestConnection(t *testing.T) {\n\n" +
	"\tt.Fatalf(\"want: connection refused\")\n}\n" +
	"-connection refused in the old note\n" +
	"+connection refused in the new note\n" +
	"```\n" + "SSE read timed out\n" + "```\n" +
	"⎿ connection refused\n"

func TestSSEStallScan_ClaudeCodeTestRunNeverMatched(t *testing.T) {
	resetSSEStallState()
	for _, source := range []string{sseStallSourceWaitLoop, sseStallSourceWatchdog} {
		if m, ok := sseStallScan("sess-claude", "claude-code", phraseTestRunScrollback, source); ok {
			t.Errorf("source %q: claude-code worker matched line %q (pattern %q); the family gate must keep this detector off claude-code", source, m.Line, m.Pattern)
		}
	}
}

func TestSSEStallScan_NonOpencodeFamiliesNeverMatched(t *testing.T) {
	for _, fam := range []string{"pi-prompt", "openwebui", "ollama", ""} {
		t.Run(fam, func(t *testing.T) {
			resetSSEStallState()
			for _, source := range []string{sseStallSourceWaitLoop, sseStallSourceWatchdog} {
				if m, ok := sseStallScan("sess-"+fam, fam, phraseTestRunScrollback, source); ok {
					t.Errorf("source %q: family %q matched line %q (pattern %q); detector must be family-gated", source, fam, m.Line, m.Pattern)
				}
			}
		})
	}
}

// TestSSEStallAppliesTo_OnlyOpencodeFamilies is the table over the backend
// family names: sseStallAppliesTo is true for exactly the three opencode
// family names, and for no other family the detector may act on.
func TestSSEStallAppliesTo_OnlyOpencodeFamilies(t *testing.T) {
	want := map[string]bool{
		"opencode":        true,
		"opencode-acp":    true,
		"opencode-prompt": true,
	}
	for _, fam := range []string{
		"opencode", "opencode-acp", "opencode-prompt",
		"claude-code", "claude", "pi-prompt", "openwebui", "ollama",
		"goose", "aider", "gemini", "",
	} {
		if got := sseStallAppliesTo(fam); got != want[fam] {
			t.Errorf("sseStallAppliesTo(%q) = %v, want %v", fam, got, want[fam])
		}
	}
}

// TestSSEStallScan_RealProviderErrorLinesMatch runs the real opencode
// provider-error lines observed in incidents (the box-drawing-wrapped
// "SSE read timed out" and the prefixed "Error: ..." / "AI_APICallError: ..."
// shapes) through both opencode family names and both scan sources: each one
// must match among TUI noise, and the match must carry the cleaned line, the
// pattern, the backend family, and the source.
func TestSSEStallScan_RealProviderErrorLinesMatch(t *testing.T) {
	cases := []struct {
		line    string
		pattern string
	}{
		{"SSE read timed out", "SSE read timed out"},
		{"Error: connection refused", "connection refused"},
		{"AI_APICallError: context deadline exceeded", "context deadline exceeded"},
	}
	for _, backend := range []string{"opencode", "opencode-acp"} {
		for _, source := range []string{sseStallSourceWaitLoop, sseStallSourceWatchdog} {
			for _, tc := range cases {
				t.Run(backend+"/"+source+"/"+tc.line, func(t *testing.T) {
					resetSSEStallState()
					scrollback := "generating…\n┃ ┃ ┃    " + tc.line + " ┃ ┃ ┃\n12.3s"
					m, ok := sseStallScan("sess-real", backend, scrollback, source)
					if !ok {
						t.Fatalf("real provider-error line %q did not match (backend %s, source %s)", tc.line, backend, source)
					}
					if m.Line != tc.line || m.Pattern != tc.pattern {
						t.Errorf("match line/pattern = %q/%q, want %q/%q", m.Line, m.Pattern, tc.line, tc.pattern)
					}
					if m.Backend != backend || m.Source != source {
						t.Errorf("match backend/source = %q/%q, want %q/%q", m.Backend, m.Source, backend, source)
					}
				})
			}
		}
	}
}

// TestSSEStallScan_AllKnownPatterns locks the regression guard: every
// phrasing in the pattern list still matches its own scrollback line, so a
// removed entry or a broken matcher cannot silently make the detector go
// blind to a known real-world error.
func TestSSEStallScan_AllKnownPatterns(t *testing.T) {
	for _, pat := range autonomousSSEStallPatterns {
		t.Run(pat, func(t *testing.T) {
			resetSSEStallState()
			scrollback := "some opencode TUI noise\n" + pat + "\nmore noise"
			m, ok := sseStallScan("sess-pattern", "opencode-acp", scrollback, sseStallSourceWatchdog)
			if !ok {
				t.Fatalf("pattern %q did not match its own scrollback line", pat)
			}
			if m.Pattern != pat {
				t.Errorf("matched pattern = %q, want %q", m.Pattern, pat)
			}
		})
	}
}

// TestSSEStallScan_CaseInsensitive: the incidents were all "a new phrasing
// wasn't in the list" bugs; casing drift specifically must not cause a
// recurrence, so the matcher compares case-insensitively.
func TestSSEStallScan_CaseInsensitive(t *testing.T) {
	resetSSEStallState()
	m, ok := sseStallScan("sess-case", "opencode", "SSE READ TIMED OUT", sseStallSourceWatchdog)
	if !ok {
		t.Fatal("expected case-insensitive match for SSE READ TIMED OUT")
	}
	if m.Pattern != "SSE read timed out" {
		t.Errorf("matched pattern = %q, want %q", m.Pattern, "SSE read timed out")
	}
}

// TestSSEStallScan_OpencodeMentionsAreNotErrors checks that an opencode
// session whose scrollback only MENTIONS the phrase — inside a code block, a
// tool result, a quoted test assertion, or a long prose line — is not
// matched: the line must BE the short one-liner opencode prints.
func TestSSEStallScan_OpencodeMentionsAreNotErrors(t *testing.T) {
	longProse := strings.Repeat("SSE read timed out showed up once in an old log excerpt the worker was summarizing, ", 6)
	cases := map[string]string{
		"code block":         "```\nSSE read timed out\n```\n",
		"tool result":        "⎿ SSE read timed out\n",
		"quoted test assert": "if err != nil { t.Fatal(\"want SSE read timed out\") }\n",
		"long prose":         longProse + "\n",
	}
	for name, sb := range cases {
		t.Run(name, func(t *testing.T) {
			resetSSEStallState()
			if m, ok := sseStallScan("sess-mention", "opencode", sb, sseStallSourceWaitLoop); ok {
				t.Errorf("mention-only scrollback (%s) matched line %q (pattern %q)", name, m.Line, m.Pattern)
			}
		})
	}
}

// TestSSEStallScan_FreshTextRule locks the old-match rule: a candidate error
// line only counts while it is NEW relative to the session's previous scan.
// The first scan of a scrollback containing the error matches; rescan with
// the identical scrollback does not; a rescan where the error line appears
// one more time than before does. The rule is per session: a different
// session id treats the same line as new, and forgetting a session resets
// its history.
func TestSSEStallScan_FreshTextRule(t *testing.T) {
	resetSSEStallState()
	const (
		base = "working on the task\n"
		err  = "SSE read timed out\n"
	)

	// First scan of a scrollback containing the error line: it is new, so it
	// matches, and the match names the line.
	m, ok := sseStallScan("sess-fresh", "opencode", base+err, sseStallSourceWaitLoop)
	if !ok {
		t.Fatal("first scan of scrollback with the error line did not match")
	}
	if m.Line != "SSE read timed out" {
		t.Errorf("matched line = %q, want the error line", m.Line)
	}

	// Identical scrollback, same session: the line is no longer new, so a
	// pane that merely CONTAINS the old error must not keep tripping.
	if _, ok := sseStallScan("sess-fresh", "opencode", base+err, sseStallSourceWaitLoop); ok {
		t.Error("rescan of identical scrollback matched; fresh-text rule should have held it")
	}

	// One more appearance of the error line than the previous scan: new
	// again, so it matches.
	if _, ok := sseStallScan("sess-fresh", "opencode", base+err+err, sseStallSourceWatchdog); !ok {
		t.Error("scan with the error line one more time than before did not match")
	}

	// A different session id has no history for this scrollback: the line is
	// new there.
	if _, ok := sseStallScan("sess-other", "opencode", base+err, sseStallSourceWaitLoop); !ok {
		t.Error("first scan of the same scrollback on a different session id did not match")
	}

	// Forgetting a session (as when it finishes) resets its history.
	sseFreshTracker.forget("sess-fresh")
	if _, ok := sseStallScan("sess-fresh", "opencode", base+err, sseStallSourceWatchdog); !ok {
		t.Error("scan after forget did not match; forget should reset the session's history")
	}
}

// The two production call sites (the verifier wait loop and the automata
// watchdog in cmd/datawatch/main.go) only run inside a live daemon: they
// read tmux pane scrollback behind the session manager and the runTask /
// watchdog goroutines, so they cannot be driven here without a daemon and
// panes. Both call sites scan through the single shared entry point
// sseStallScan, passing their own source constant — driving that entry
// point with both sources is the call-site parity.
func TestSSEStallScan_SourceParity(t *testing.T) {
	scrollbacks := map[string]string{
		"real error": "generating…\n┃ ┃ ┃    SSE read timed out ┃ ┃ ┃\n",
		"prefixed":   "Error: connection refused\n",
		"mention":    "```\nSSE read timed out\n```\n",
		"noise only": "working on the task\n",
	}
	for _, backend := range []string{"opencode", "opencode-acp", "claude-code"} {
		for name, sb := range scrollbacks {
			t.Run(backend+":"+name, func(t *testing.T) {
				resetSSEStallState()
				// Separate session ids so the shared fresh-tracker state
				// does not interfere between the two scan sources.
				mWait, waitOK := sseStallScan("parity-wait", backend, sb, sseStallSourceWaitLoop)
				mWatch, watchOK := sseStallScan("parity-watch", backend, sb, sseStallSourceWatchdog)
				if waitOK != watchOK {
					t.Fatalf("match results differ by source: wait loop %v, watchdog %v", waitOK, watchOK)
				}
				if !waitOK {
					return
				}
				if mWait.Pattern != mWatch.Pattern || mWait.Line != mWatch.Line || mWait.Backend != mWatch.Backend {
					t.Errorf("matches differ by source: %+v vs %+v", mWait, mWatch)
				}
				if mWait.Source != sseStallSourceWaitLoop || mWatch.Source != sseStallSourceWatchdog {
					t.Errorf("source names = %q/%q, want the two call-site sources", mWait.Source, mWatch.Source)
				}
			})
		}
	}
}

// TestSSEStallScan_NoFalsePositiveOnNoise guards against the other half of
// the detector's job: ordinary worker output must not match.
func TestSSEStallScan_NoFalsePositiveOnNoise(t *testing.T) {
	resetSSEStallState()
	scrollback := "reading the request\nimplementing the change\nrunning tests\nall 142 tests pass\n"
	if m, ok := sseStallScan("sess-noise", "opencode", scrollback, sseStallSourceWaitLoop); ok {
		t.Errorf("normal output matched line %q (pattern %q)", m.Line, m.Pattern)
	}
}
