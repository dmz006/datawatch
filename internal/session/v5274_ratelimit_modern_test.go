// v5.27.4 — operator-reported regression: rate-limit detection
// broke for the modern claude-code "limit reached" phrasings.
// These tests lock in the per-backend table in rate_limit_lines.go so
// the auto-schedule resume pipeline fires for both the legacy
// "You've hit your limit" form and the current "5-hour limit reached" /
// "weekly limit reached" forms — and does NOT fire on lines that merely
// quote those phrasings mid-sentence.

package session

import (
	"strings"
	"testing"
)

// matchesRateLimit replicates the in-flight check in processOutputLine
// for the claude-code family this file's fixtures come from: a hit is
// the shared protocol marker at line start, else — under the 2048-char
// gate — one of the family's recorded phrases at line start, or after
// the family's own "⎿  " gutter prefix (rate_limit_lines.go).
// Matched case-insensitively, anchored, no substring-anywhere.
func matchesRateLimit(line string) bool {
	low := strings.ToLower(line)
	if strings.HasPrefix(low, builtinRateLimitMarkerLower) {
		return true
	}
	if len(line) >= 2048 {
		return false
	}
	return matchesBuiltinRateLimitLines("claude-code", low)
}

func TestRateLimitPatterns_ModernPhrasings(t *testing.T) {
	hits := []string{
		// Legacy phrasings (regression check that v5.27.4 didn't drop them)
		"You've hit your limit",
		"You've hit your limit ∙ resets 2pm (America/New_York)",
		"rate limit exceeded",
		"quota exceeded",
		// v5.27.4 new phrasings — modern claude-code:
		"5-hour limit reached ∙ resets 2pm",
		"5-hour limit reached",
		"Weekly limit reached",
		"Weekly usage limit reached. Resets in 2 days.",
		"Hit weekly limit on opus.",
		"Opus limit reached",
		"Sonnet limit reached",
		// WARN-tier phrasings (older patterns, kept)
		"Approaching usage limit",
		"limit will reset at 9pm",
		// Same dialog text as a pane capture: anchored on claude-code's
		// own "⎿  " gutter prefix (the captured fixture's exact form).
		"⎿  You've hit your limit ∙ resets 10pm (America/New_York)",
	}
	for _, line := range hits {
		if !matchesRateLimit(line) {
			t.Errorf("expected rate-limit MATCH on %q, got miss", line)
		}
	}
}

func TestRateLimitPatterns_NoFalsePositives(t *testing.T) {
	misses := []string{
		"",
		"echo hello",
		"running tests",
		"the file size limit is 10MB", // "limit" alone
		"Calling api.example.com to fetch latest data", // unrelated
		// Quoting a recorded phrase mid-sentence is not a detection —
		// the anchored table must not fire on these.
		"task says: when you see 'You've hit your limit' in the log, note the time",
		"operator notes: 'rate limit exceeded' is handled elsewhere",
		// Marker quoted mid-line (not at line start) is not a detection.
		"echo 'DATAWATCH_RATE_LIMITED: resets at 12:34' >> notes.txt",
		// Truncation guard: very long line shouldn't be checked
		strings.Repeat("X ", 150), // > 200 chars
	}
	for _, line := range misses {
		if matchesRateLimit(line) {
			t.Errorf("expected rate-limit MISS on %q, got hit", line)
		}
	}
}

func TestParseRateLimitResetTime_ModernResetMarker(t *testing.T) {
	// "5-hour limit reached ∙ resets 2pm" — the parser's family-2
	// "resets " marker should still find the clock token.
	line := "5-hour limit reached ∙ resets 2pm"
	got := parseRateLimitResetTime(line)
	if got.IsZero() {
		t.Fatalf("expected reset time parsed from %q, got zero", line)
	}
	if got.Hour() != 14 || got.Minute() != 0 {
		t.Errorf("got hour=%d minute=%d want 14:00 (2pm)", got.Hour(), got.Minute())
	}
}

func TestParseRateLimitResetTime_WeeklyResets(t *testing.T) {
	// Weekly limit messages often use relative form.
	line := "Weekly usage limit reached. Resets in 2 days."
	got := parseRateLimitResetTime(line)
	// Family-3 relative "resets in" should match "2 days" — the
	// existing parser handles "30m" / "4h 23m" but may not handle
	// "2 days" yet. If it doesn't, that's expected; the auto-resume
	// falls back to +60min with a warning. Document the current
	// behaviour either way so a future parser extension knows what
	// to aim for.
	t.Logf("weekly-relative parse result: zero=%v at=%v", got.IsZero(), got)
}
