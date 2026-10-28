package session

// BL406 Phase 1 — v5.27.4-style "modern phrasing" regression tests,
// migrated onto the per-backend anchored table (rate_limit_lines.go).
//
// v5.27.4 added detection of the modern claude-code throttle phrasings
// ("5-hour limit reached", "Hit weekly limit", "Opus limit reached", …);
// these tests keep that true-detection coverage. Matching is anchored at
// the line start (case-insensitive, after the family's recorded "⎿  "
// gutter), so only lines that LEAD with a recorded phrase are
// detections — the recordings are in testdata/rate_limit_lines/ with
// per-line provenance in the README. Phrasings from the era before any of
// those in-tree recordings (e.g. "rate limit exceeded", "quota exceeded")
// are no longer built in: they belong to the operator-configured flat
// list, whose classic substring-anywhere semantics for all backends are
// pinned by TestRateLimitOperatorFlatListKeepsSubstringBehavior in
// rate_limit_lines_test.go.

import (
	"strings"
	"testing"
)

// matchesRateLimit mirrors the manager's detection path for a
// claude-code session without driving the full state machine: the shared
// protocol marker fires whenever the trimmed line starts with it
// (no length gate), then the 2048-byte gate applies, then the anchored
// per-backend table match.
func matchesRateLimit(line string) bool {
	low := strings.ToLower(strings.TrimSpace(line))
	if strings.HasPrefix(low, builtinRateLimitMarkerLower) {
		return true
	}
	if len(line) >= 2048 {
		return false
	}
	return matchesBuiltinRateLimitLines("claude-code", low)
}

func TestRateLimitDetector_5hWeeklyModernPhrasings(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		// True detections: modern operator-reported phrasings leading the
		// line (verbatim sources in testdata/rate_limit_lines/README.md).
		{"hit-your-limit", "You've hit your limit", true},
		{"hit-your-limit-reset", "You've hit your limit ∙ resets 2pm (America/New_York)", true},
		{"5h-reset", "5-hour limit reached ∙ resets 2pm", true},
		{"5h-dialog", "5-hour limit reached. Use the limit reset time to plan upcoming work. Resets at 2pm.", true},
		{"weekly-reached", "Weekly limit reached", true},
		{"hit-weekly-oped", "Hit weekly limit on opus.", true},
		{"opus-reached", "Opus limit reached", true},
		{"sonnet-reached", "Sonnet limit reached", true},
		{"gutter-form", "⎿  You've hit your limit ∙ resets 10pm (America/New_York)", true},
		{"protocol-marker", "DATAWATCH_RATE_LIMITED: resets 2026-10-28T16:00:00Z", true},

		// False positives: a phrase quoted somewhere else on the line is
		// not a throttle notice under anchored matching.
		{"quoted-print-output", "I checked what the security workflow prints; it outputs You've hit your limit and then sleeps (10s)", false},
		{"quota-docs-chat", "I found 3 references to the quota in the logs, and the docs say quota exceeded is common", false},
		{"weekly-usage-dashboard", "I hit the weekly limit last week but the dashboard still shows 85% of my quota used", false},
		{"error-log-errata", "The error log says 'rate limit exceeded' but the API docs suggest it should be 'usage limit will reset' instead", false},
		{"subscription-pending", "Your subscription renewal is pending; the 5-hour limit does not apply to this session", false},
		{"weekly-usage-page", "The weekly usage limit page lists plan reset details for all seats", false},
		{"task-notes-opus", "Task notes: user asks about the opus limit reached error and how to configure a fallback to sonnet", false},
		{"previous-session-log", "A previous session logged Sonnet limit reached before the upgrade; no action needed", false},
		{"notes-banner-text", "notes: 'You're out of extra usage' is the banner text — see the operator guide for the reset flow", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesRateLimit(tc.line); got != tc.want {
				t.Errorf("matchesRateLimit(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestRateLimitDetector_LongLineGuard(t *testing.T) {
	// The 2048-byte gate keeps paragraph-length output from being matched
	// as a candidate throttle notice (BL215 history: the old 200-byte
	// gate dropped ~600-byte claude dialogs, so the modern gate is
	// 2048).
	if matchesRateLimit(strings.Repeat("x", 3000)) {
		t.Error("expected a 3000-byte line to be excluded by the length gate")
	}

	// The shared protocol marker is not subject to the gate.
	if !matchesRateLimit("DATAWATCH_RATE_LIMITED: resets 2026-10-28T16:00:00Z") {
		t.Error("protocol marker must detect regardless of the gate")
	}
}
