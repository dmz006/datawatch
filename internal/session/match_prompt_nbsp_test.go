package session

import "testing"

// TestInputRowIndex_FindsRowDrawnWithNoBreakSpace: Claude Code 2.1.331 draws
// the input row as "❯", a no-break space, then the suggestion. The active
// spinner pre-check of matchPromptLines locates the input row first; testing
// the "❯ " prefix with an ASCII space only never finds the real row, so the
// spinner above it is never read. The row must be found whichever space follows.
func TestInputRowIndex_FindsRowDrawnWithNoBreakSpace(t *testing.T) {
	for name, gap := range map[string]string{"ascii space": " ", "no-break space": " ", "bare": ""} {
		lines := []string{
			"✻ Pondering…",
			"──────────────────────────────",
			"❯" + gap + `Try "fix the failing test"`,
			"──────────────────────────────",
			"  ⏵⏵ bypass permissions on",
		}
		if gap == "" {
			lines[2] = "❯"
		}
		if got := inputRowIndex(lines); got != 2 {
			t.Errorf("%s: input row index = %d, want 2", name, got)
		}
	}
	// A line that only starts with the glyph and runs on is not an input row.
	if got := inputRowIndex([]string{"❯1. Yes"}); got != -1 {
		t.Errorf("glyph without a gap matched as an input row: %d", got)
	}
}

// TestMatchPromptLines_SpinnerAboveNoBreakSpaceRowSuppressesPrompt: with the
// real no-break-space row, an active spinner above it still suppresses a
// prompt match that would otherwise be reported from the lines around it.
func TestMatchPromptLines_SpinnerAboveNoBreakSpaceRowSuppressesPrompt(t *testing.T) {
	m := &Manager{}
	for name, gap := range map[string]string{"ascii space": " ", "no-break space": " "} {
		lines := []string{
			"Do you want to proceed?",
			"✻ Pondering…",
			"❯" + gap + `Try "fix the failing test"`,
		}
		if matched, _ := m.matchPromptLines(lines, 10, true); matched != "" {
			t.Errorf("%s: active spinner above the input row, yet matched a prompt: %q", name, matched)
		}
	}
}
