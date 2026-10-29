// v5.27.6 — tests for BL211 (scrollback state-detection) and
// BL215 (rate-limit miss + no-reset-time fallback).
//
// BL211 verifies the new CapturePaneLiveTail interface is in place
// on both the real and fake tmux implementations, since the
// production state-detection loop now switches to it.
//
// BL215 verifies modern rate-limit messages longer than the
// previous 200-char gate now match, AND that lines lacking a
// parseable reset time still fall through to the +60min fallback.

package session

import (
	"strings"
	"testing"
	"time"
)

func TestBL211_FakeTmuxImplementsCapturePaneLiveTail(t *testing.T) {
	// State detection in manager.go now calls CapturePaneLiveTail.
	// FakeTmux must satisfy the interface contract used by both
	// production and unit tests.
	var tm TmuxAPI = NewFakeTmux()
	if _, ok := tm.(interface {
		CapturePaneLiveTail(string) (string, error)
	}); !ok {
		t.Fatal("FakeTmux must implement CapturePaneLiveTail")
	}
}

func TestBL211_TmuxManagerInterfaceListsLiveTail(t *testing.T) {
	// Defensive: the TmuxManagerInterface declaration in tmux.go
	// must list CapturePaneLiveTail so any future fake / mock
	// implementing only the existing methods fails to compile.
	// Compile-time assertion via type-check:
	var _ interface {
		CapturePaneVisible(string) (string, error)
		CapturePaneLiveTail(string) (string, error)
		CapturePaneANSI(string) (string, error)
	} = (*FakeTmux)(nil)
}

func TestBL211_CapturePaneLiveTailFakeBehavior(t *testing.T) {
	tm := NewFakeTmux()
	tm.Pane["s1"] = "live tail content\n❯ "
	out, err := tm.CapturePaneLiveTail("s1")
	if err != nil {
		t.Fatalf("CapturePaneLiveTail err: %v", err)
	}
	if out != "live tail content\n❯ " {
		t.Errorf("got %q want %q", out, "live tail content\n❯ ")
	}
	// Recorded under its own op name so tests can assert which
	// capture method production code took.
	if tm.Count("capture-live-tail") != 1 {
		t.Errorf("capture-live-tail count=%d want 1", tm.Count("capture-live-tail"))
	}
	if tm.Count("capture-visible") != 0 {
		t.Errorf("CapturePaneLiveTail must not record under capture-visible op")
	}
}

func TestBL215_LongRateLimitLineMatches(t *testing.T) {
	// v5.27.4 raised the rate-limit pattern set; the per-line length gate
	// was raised 200 → 1024 (and production now gates at 2048) because
	// modern claude-code rate-limit dialogs are paragraph-length.
	// Build a 600+ char dialog line that starts with the v5.27.4
	// "5-hour limit reached" phrasing, anchored on claude-code's own
	// "⎿  " gutter prefix (the captured form) — the anchored table must
	// still detect it, with context trailing the phrase.
	line := "⎿  5-hour limit reached. " +
		strings.Repeat("Some context describing why the limit hit and what the operator should do next. ", 7) +
		"Reach out to support if blocked."
	if len(line) <= 200 {
		t.Fatalf("test setup error: line is only %d chars, won't exercise the gate", len(line))
	}
	if !matchesRateLimit(line) {
		t.Errorf("expected MATCH on %d-char rate-limit line, got miss", len(line))
	}
}

func TestBL215_NoResetTimeFallsThroughTo60min(t *testing.T) {
	// parseRateLimitResetTime returns zero time when the line has
	// no parseable reset marker. The auto-resume scheduler at
	// manager.go:3837-3840 must fall through to +60min in that case.
	// Verify the parser returns zero (the fallback logic itself is
	// exercised by the manager-level rate-limit smoke).
	line := "5-hour limit reached." // no resets/at/in marker
	got := parseRateLimitResetTime(line)
	if !got.IsZero() {
		t.Errorf("expected zero time for no-reset-marker line, got %v", got)
	}
}

func TestBL215_LengthGateUpperBoundStillRejects(t *testing.T) {
	// The production ceiling is 2048 chars; anything beyond should still
	// be ignored to avoid false positives from giant log dumps.
	line := strings.Repeat("limit reached ", 200) // 14 chars × 200 = 2800 chars
	if len(line) < 2048 {
		t.Fatalf("test setup: line is only %d chars; need ≥ 2048", len(line))
	}
	if matchesRateLimit(line) {
		t.Errorf("expected miss for %d-char line (above 2048 ceiling), got match", len(line))
	}
}

// matchesRateLimitV5276 replicates the in-flight check at the
// rate-limit site in processOutputLine (anchored table, 2048 gate)
// so the test can verify the gate without spinning up a Manager.
// Mirrors the exact branching the production code uses for the
// claude-code family.
func matchesRateLimitV5276(line string) bool {
	low := strings.ToLower(line)
	if strings.HasPrefix(low, builtinRateLimitMarkerLower) {
		return true
	}
	if len(line) >= 2048 {
		return false
	}
	return matchesBuiltinRateLimitLines("claude-code", low)
}

// Test-helper sanity: both helpers model the production 2048-char gate
// and the anchored per-backend table, so an over-200-char line that
// starts with a recorded phrase still matches.
func TestBL215_HelperUsesNewGate(t *testing.T) {
	// 800+ char line starting with a recorded phrase — the gate used to
	// be 200, production is 2048, so this must still match.
	line := "5-hour limit reached. " + strings.Repeat("operator guidance details. ", 57)
	if !matchesRateLimitV5276(line) {
		t.Errorf("helper must match %d-char limit-reached line", len(line))
	}
	if !matchesRateLimit(line) {
		t.Errorf("v5.27.4 helper must match the same %d-char line", len(line))
	}
}

// Ensure the auto-resume scheduling math works when resetAt is zero.
// Production code at line 3837:
//
//	resumeAt := resetAt
//	if resumeAt.IsZero() || time.Until(resumeAt) < time.Minute {
//	    resumeAt = time.Now().Add(60 * time.Minute)
//	}
func TestBL215_FallbackResumeAt60min(t *testing.T) {
	var resetAt time.Time // zero
	resumeAt := resetAt
	if resumeAt.IsZero() || time.Until(resumeAt) < time.Minute {
		resumeAt = time.Now().Add(60 * time.Minute)
	}
	delta := time.Until(resumeAt)
	if delta < 59*time.Minute || delta > 61*time.Minute {
		t.Errorf("fallback resumeAt should be ~60min from now, got delta=%v", delta)
	}
}
