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

// v5.26.31 — operator-reported regression: "Last response is now only
// garbage and not any text from the last set of responses." The
// v5.26.23 filter was too charitable: it kept any line with hasWord3
// AND only matched footers anchored to line start. So lines like
//   `⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt`
// passed through (leading `⏵⏵` defeated the anchored prefix), and
//   `* Perambulating… (11m 42s · ↓ 22.2k tokens · thinking)`
// passed through (Perambulating gives hasWord3, no anchored match).
// v5.26.31 broadens the noise-pattern check to apply unconditionally
// and adds three new structural detectors (labeled border, embedded
// status timer, single-spinner counter). Trade-off: the rare
// "the doc says press esc to interrupt" prose CAN now be filtered.
// Operator volume of noise complaints made this the right call.
func TestStripResponseNoise_DropsSession_eac4_OperatorReport(t *testing.T) {
	// Verbatim from /api/sessions/response on a running claude-code
	// session that was still thinking when the operator inspected it.
	in := strings.Join([]string{
		"✻                                2",
		"✢                      1",
		"                       2",
		"     (ctrl+b ctrl+b (twice) to run in background)",
		"* Perambulating… (11m 42s · ↓ 22.2k tokens · almost done thinking with high effort)",
		"──────────────────────────────────────────────────── datawatch claude ──",
		"  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt",
		"✶                      3",
	}, "\n")
	got := stripResponseNoise(in)
	if got != "" {
		t.Errorf("expected pure-noise tail to filter to empty; got: %q", got)
	}
}

func TestStripResponseNoise_KeepsRealProseAroundNoise(t *testing.T) {
	// Real answer mixed with noise — prose stays, noise drops.
	in := strings.Join([]string{
		"Here is the analysis you asked for.",
		"✻                                2",
		"The function takes three arguments and returns a tuple.",
		"  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt",
	}, "\n")
	got := stripResponseNoise(in)
	if !strings.Contains(got, "Here is the analysis") {
		t.Errorf("dropped real prose: %q", got)
	}
	if !strings.Contains(got, "three arguments") {
		t.Errorf("dropped second prose line: %q", got)
	}
	if strings.Contains(got, "bypass permissions") || strings.Contains(got, "✻") {
		t.Errorf("kept noise: %q", got)
	}
}

func TestStripResponseNoise_BareFooterDrops(t *testing.T) {
	if got := stripResponseNoise("esc to interrupt"); got != "" {
		t.Errorf("bare footer not dropped: %q", got)
	}
}
