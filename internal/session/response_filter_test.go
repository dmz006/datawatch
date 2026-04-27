// v5.26.15 — operator-reported: response capture should filter out
// animation spinning icons and TUI noise. stripResponseNoise tests.

package session

import (
	"strings"
	"testing"
)

func TestStripResponseNoise_DropsSpinnerLines(t *testing.T) {
	in := strings.Join([]string{
		"●",
		"Here is the actual response that should survive.",
		"✢",
		"With a second useful line.",
		"⠋",
	}, "\n")
	got := stripResponseNoise(in)
	if !strings.Contains(got, "Here is the actual response") {
		t.Errorf("dropped useful line: %q", got)
	}
	if !strings.Contains(got, "second useful line") {
		t.Errorf("dropped second useful line: %q", got)
	}
	if strings.Contains(got, "●") || strings.Contains(got, "✢") || strings.Contains(got, "⠋") {
		t.Errorf("kept spinner glyph: %q", got)
	}
}

func TestStripResponseNoise_DropsStatusTimer(t *testing.T) {
	in := strings.Join([]string{
		"running task: hostname read",
		"(7s · timeout 1m)",
		"(esc to interrupt)",
		"hostname is ralfthewise",
	}, "\n")
	got := stripResponseNoise(in)
	if !strings.Contains(got, "hostname is ralfthewise") {
		t.Errorf("dropped real content: %q", got)
	}
	if strings.Contains(got, "timeout 1m") {
		t.Errorf("kept timeout fragment: %q", got)
	}
	if strings.Contains(got, "esc to interrupt") {
		t.Errorf("kept esc-footer: %q", got)
	}
}

func TestStripResponseNoise_PreservesProseWithLeadingPunct(t *testing.T) {
	// Bullet-list lines should NOT be dropped just because they
	// start with `*` — only PURE-spinner lines (the line is exactly
	// `*`) get filtered.
	in := strings.Join([]string{
		"Summary:",
		"* first finding",
		"* second finding",
		"*",
		"final note",
	}, "\n")
	got := stripResponseNoise(in)
	if !strings.Contains(got, "* first finding") {
		t.Errorf("dropped bullet: %q", got)
	}
	if !strings.Contains(got, "* second finding") {
		t.Errorf("dropped bullet: %q", got)
	}
	if strings.Contains(got, "\n*\n") || strings.HasSuffix(got, "\n*") {
		t.Errorf("kept lone-star spinner line: %q", got)
	}
}

func TestStripResponseNoise_StripsANSI(t *testing.T) {
	in := "\x1b[31mError:\x1b[0m something broke"
	got := stripResponseNoise(in)
	if strings.Contains(got, "\x1b") {
		t.Errorf("ANSI not stripped: %q", got)
	}
	if !strings.Contains(got, "Error:") || !strings.Contains(got, "something broke") {
		t.Errorf("dropped real content: %q", got)
	}
}

func TestStripResponseNoise_CollapsesBlankRuns(t *testing.T) {
	in := "line1\n\n\n\n\nline2"
	got := stripResponseNoise(in)
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("did not collapse blank runs: %q", got)
	}
}

func TestStripResponseNoise_EmptyInput(t *testing.T) {
	if got := stripResponseNoise(""); got != "" {
		t.Errorf("empty input → %q, want empty", got)
	}
	if got := stripResponseNoise("●\n⠋\n*"); got != "" {
		t.Errorf("all-noise input → %q, want empty", got)
	}
}

// v5.26.23 — operator-reported: real prose framed by claude's TUI
// borders was getting dropped because the v5.26.15 filter matched
// box-drawing chars anywhere in the line. The fix: prose-detection
// (3+ consecutive letters) overrides decoration.
func TestStripResponseNoise_PreservesProseInBoxBorder(t *testing.T) {
	in := strings.Join([]string{
		"╭────────────────────────╮",
		"│  Here is your answer   │",
		"│  with multiple lines    │",
		"╰────────────────────────╯",
	}, "\n")
	got := stripResponseNoise(in)
	if !strings.Contains(got, "Here is your answer") {
		t.Errorf("dropped boxed prose: %q", got)
	}
	if !strings.Contains(got, "with multiple lines") {
		t.Errorf("dropped second boxed prose line: %q", got)
	}
	// Pure-decoration top + bottom lines should still be filtered.
	if strings.Contains(got, "╭───") || strings.Contains(got, "╰───") {
		t.Errorf("kept pure-decoration border: %q", got)
	}
}

// v5.26.23 — operator-reported example: pane-capture wide rows with
// multiple progress markers strewn across columns. Real prose
// almost never has 2+ spinner glyphs; drop those lines.
func TestStripResponseNoise_DropsMultiSpinnerLine(t *testing.T) {
	in := strings.Join([]string{
		"Ex50 ✶            1 ✽            2 ✢            3",
		"Real answer line goes here.",
	}, "\n")
	got := stripResponseNoise(in)
	if strings.Contains(got, "✶") || strings.Contains(got, "✽") || strings.Contains(got, "✢") {
		t.Errorf("multi-spinner line not dropped: %q", got)
	}
	if !strings.Contains(got, "Real answer line goes here") {
		t.Errorf("dropped real answer line: %q", got)
	}
}

// v5.26.23 — make sure the v5.26.15 anchored-footer behavior is
// preserved: prose that MENTIONS a footer phrase isn't filtered, but
// the footer itself is.
func TestStripResponseNoise_FooterAnchoringIsPositional(t *testing.T) {
	// Bare footer at line start — drop.
	if got := stripResponseNoise("esc to interrupt"); got != "" {
		t.Errorf("bare footer not dropped: %q", got)
	}
	// Prose that mentions the phrase mid-sentence — keep.
	in := "If you want to halt the operation, press esc to interrupt."
	if got := stripResponseNoise(in); !strings.Contains(got, "esc to interrupt") {
		t.Errorf("prose that mentions footer phrase got filtered: %q", got)
	}
}
