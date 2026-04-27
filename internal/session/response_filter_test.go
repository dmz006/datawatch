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
