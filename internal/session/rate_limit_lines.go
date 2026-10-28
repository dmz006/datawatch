// Package session provides the session manager. This file holds the
// built-in rate-limit line table that replaces the old generic substring
// list (formerly rateLimitPatterns in manager.go).
//
// Matching rule (shared by every backend)
// ---------------------------------------
// processOutputLine receives each already-trimmed, ANSI-stripped pane
// line. A line is a rate-limit signal when:
//
//  1. Shared protocol marker — the line starts with
//     builtinRateLimitMarker (case-insensitive), regardless of backend.
//     A line that merely quotes the marker mid-sentence does not match.
//
//  2. Backend table — the session's BackendFamily has one or more
//     phrases in builtinRateLimitPhrases, and the line starts with one
//     of them, or with the family's own recorded line prefix
//     (builtinRateLimitLinePrefix) followed by one of them, compared
//     case-insensitively. A line that merely quotes a phrase mid-sentence
//     is never a detection.
//
// An operator-configured rate_limit_patterns (config global or per-LLM)
// is a separate flat list that keeps the classic substring behavior, for
// lines under the same 2048-char gate; it is not part of this table.
//
// Provenance
// ----------
// Every phrase below comes from an in-tree record of lines claude-code
// actually printed: the captured line in
// testdata/rate_limit_lines/claude-code.txt, the operator-reported
// phrasings recorded in the v5.27.4 / v6.2.0 / v6.11.3 release notes and
// CHANGELOG entries, and the operator reproductions in the in-tree tests
// (v5274/v5276/BL262). Nothing here is invented; phrases are recorded
// in their original casing and compared case-insensitively.
//
// Observed gaps
// -------------
// The backends below have no captured line in testdata/rate_limit_lines
// yet, so their table entries are empty: the shared protocol marker is
// the only built-in rule that fires for them. When a real throttle line
// is captured for any of them, it belongs in the table with its
// provenance recorded here.
package session

import (
	"sort"
	"strings"
)

// builtinRateLimitMarker is the shared explicit protocol entry: any
// backend may emit it, and it is recognized only as a line start.
const builtinRateLimitMarker = "DATAWATCH_RATE_LIMITED:"

// builtinRateLimitMarkerLower is the lowercased comparison form of
// builtinRateLimitMarker.
const builtinRateLimitMarkerLower = "datawatch_rate_limited:"

// builtinRateLimitPhrases is the built-in per-backend rate-limit line
// table, keyed by BackendFamily. A phrase is a recorded line start from
// the provenance sources in the file comment; matching is anchored (the
// line starts with the phrase, or with the family's line prefix followed
// by the phrase), case-insensitive. Empty entries are the observed gaps:
// those backends rely on the shared protocol marker until a real line is
// captured.
var builtinRateLimitPhrases = map[string][]string{
	"claude-code": {
		"You've hit your limit", // testdata/rate_limit_lines/claude-code.txt: "⎿  You've hit your limit · resets 10pm (America/New_York)"
		"rate limit exceeded",   // legacy phrasing (v5.27.4 notes; seeded default since v5.25.5)
		"quota exceeded",        // legacy phrasing (v5.25.5 CHANGELOG; seeded default)
		"usage limit will reset", // legacy dialog wording (v5.27.4 notes; seeded default)
		"Approaching usage limit", // WARN dialog (~80% of capacity, v5.27.4 notes)
		"limit will reset at", // reset-time announcement, recorded v5.27.4
		"limit reached", // "5-hour limit reached" / "Weekly limit reached" (v5.27.4, operator-reported)
		"Weekly limit reached", // captured v5.27.4 phrasing
		"Weekly usage limit", // "Weekly usage limit reached" (v5.27.4)
		"Hit weekly limit", // "Hit weekly limit" (v5.27.4)
		"5-hour limit", // 5-hour-window phrasing (v5.27.4)
		"opus limit reached", // model-specific phrasing (v5.27.4)
		"sonnet limit reached", // model-specific phrasing (v5.27.4)
		"claude usage limit", // "Claude usage limits" dialog header, observed 2026-05-03 (v6.2.0)
		"you've exceeded", // "You've exceeded your Claude plan" (v6.2.0 record)
		"plan limit", // "plan limit reached" variants (v6.2.0 record)
		"resets at", // reset-time announcement, "Resets at 2pm" (v5.27.4 notes)
		"stop and wait", // dialog option-1 text (v6.11.x record)
		"wait for limit to reset", // option 1 text when the dialog wraps (v6.11.3 record)
		"out of extra usage", // "You're out of extra usage · resets 11:50am (America/New_York)" (v6.11.3, operator-reported)
		"you're out of", // wider catch: "You're out of credits/quota/usage" (v6.11.3)
	},

	// Observed gaps: no captured line for these backends yet, so only
	// the shared protocol marker is the built-in rule today.
	"pi-prompt":       nil,
	"opencode":        nil,
	"opencode-acp":    nil,
	"opencode-prompt": nil,
	"ollama":          nil,
}

// builtinRateLimitLinePrefix holds a backend's own error prefix exactly
// as it appears in the captured records: for claude-code the "⎿" gutter
// glyph plus two spaces, e.g. the fixture line "⎿  You've hit your limit
// · resets 10pm (America/New_York)" and the operator-reproduced "⎿  You're
// out of extra usage · resets 11:50am (America/New_York)". A captured
// line whose phrase cannot be caught by a line-start rule alone is
// anchored on this prefix instead: the line starts with the prefix
// followed by one of the family's phrases.
var builtinRateLimitLinePrefix = map[string]string{
	"claude-code": "⎿  ",
}

// matchesBuiltinRateLimitLines reports whether lowerLine (the pane line
// already lowercased by the caller, and already trimmed / ANSI-stripped by
// processOutputLine) is a built-in rate-limit signal for backend family
// backendFamily: either the shared protocol marker at line start, or one
// of the family's phrases at line start, or the family's recorded line
// prefix followed by one of the family's phrases. Quoting a phrase
// mid-sentence never matches.
func matchesBuiltinRateLimitLines(backendFamily string, lowerLine string) bool {
	if strings.HasPrefix(lowerLine, builtinRateLimitMarkerLower) {
		return true
	}
	prefix := strings.ToLower(builtinRateLimitLinePrefix[backendFamily])
	for _, phrase := range builtinRateLimitPhrases[backendFamily] {
		lp := strings.ToLower(phrase)
		if strings.HasPrefix(lowerLine, lp) {
			return true
		}
		if prefix != "" && strings.HasPrefix(lowerLine, prefix+lp) {
			return true
		}
	}
	return false
}

// builtinRateLimitPatternsFlat flattens the per-backend table into a
// plain list for fingerprinting: the shared marker, each backend family
// name the table is keyed by, and that family's phrases, in sorted
// family order. Because the backend names are part of the input, any
// change to a built-in entry — a phrase or the backend it belongs to —
// changes the patterns_version digest (see patterns_version.go). Matching
// itself stays per-backend: see matchesBuiltinRateLimitLines.
func builtinRateLimitPatternsFlat() []string {
	families := make([]string, 0, len(builtinRateLimitPhrases))
	for family := range builtinRateLimitPhrases {
		families = append(families, family)
	}
	sort.Strings(families)
	out := make([]string, 0, 16)
	out = append(out, builtinRateLimitMarker)
	for _, family := range families {
		out = append(out, family)
		out = append(out, builtinRateLimitPhrases[family]...)
	}
	return out
}
