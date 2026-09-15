package main

import "testing"

// TestMatchSSEStallPattern_B89Regression locks in the exact scrollback
// fragment observed in the B89 incident (2026-09-15): opencode's
// @ai-sdk/openai-compatible client aborted a qwen3.8:27b generation after a
// 6m9s silent "thinking" pause exceeded opencode's default 300s
// chunkTimeout, printing "SSE read timed out". The watchdog's pattern list
// did not include this phrasing, so the session stalled ~2h undetected
// until an operator manually killed it. This test fails if that exact
// string (or its casing) ever stops matching again.
func TestMatchSSEStallPattern_B89Regression(t *testing.T) {
	scrollback := "┃ ┃ ┃    SSE read timed out ┃ ┃ ┃"
	pat, ok := matchSSEStallPattern(scrollback)
	if !ok {
		t.Fatalf("matchSSEStallPattern did not match observed B89 scrollback: %q", scrollback)
	}
	if pat != "SSE read timed out" {
		t.Errorf("matched pattern = %q, want %q", pat, "SSE read timed out")
	}
}

func TestMatchSSEStallPattern_AllKnownPatterns(t *testing.T) {
	for _, pat := range autonomousSSEStallPatterns {
		t.Run(pat, func(t *testing.T) {
			scrollback := "some opencode TUI noise\n" + pat + "\nmore noise"
			if _, ok := matchSSEStallPattern(scrollback); !ok {
				t.Errorf("pattern %q did not match its own scrollback fragment", pat)
			}
		})
	}
}

func TestMatchSSEStallPattern_CaseInsensitive(t *testing.T) {
	// B83/B85/B89 were all "a new phrasing wasn't in the list" bugs. Casing
	// drift specifically should never cause a fourth recurrence.
	scrollback := "sse read TIMED OUT while streaming"
	if _, ok := matchSSEStallPattern(scrollback); !ok {
		t.Errorf("expected case-insensitive match for %q", scrollback)
	}
}

func TestMatchSSEStallPattern_NoFalsePositiveOnNormalOutput(t *testing.T) {
	scrollback := "Writing docs/plans/harness-impl/eval-sweep-spec.md\nDone. DATAWATCH_COMPLETE: wrote spec file."
	if pat, ok := matchSSEStallPattern(scrollback); ok {
		t.Errorf("unexpected stall match %q on normal completion output", pat)
	}
}
