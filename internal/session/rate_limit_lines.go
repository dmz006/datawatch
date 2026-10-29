// Package session provides the session manager. This file defines the
// built-in per-backend rate-limit line table that replaces the former
// generic substring list.
//
// Matching rules (apply to every backend)
// ----------------------------------------
// processOutputLine trims the pane line of leading and trailing
// whitespace before any rate-limit check (see manager.go). A line is a
// rate-limit signal when:
//
//  1. Shared protocol marker — the trimmed line starts with
//     builtinRateLimitMarker, case-insensitive (rule for every backend,
//     including ones with no captured phrases yet). A line that only
//     quotes the marker mid-sentence does not match.
//
//  2. Backend table — the session's BackendFamily has
//     builtinRateLimitPhrases entries and the trimmed line starts with
//     one of them (or with one of that family's documented line
//     prefixes from builtinRateLimitLinePrefixes followed by one of
//     them), case-insensitive. Comparisons are byte-exact otherwise: the table
//     does not normalize Unicode, so each phrase carries the exact
//     glyph it was captured with (⎿ U+237F, · U+00B7, ∙ U+22C5).
//
// Quoting a phrase anywhere else on the line is never a detection, for
// any backend. An operator-configured flat list
// (detection.rate_limit_patterns, global or per-LLM) is separate, keeps
// the classic substring-anywhere semantics for ALL backends, and is not
// part of this table.
//
// Provenance
// ----------
// Every phrase is recorded verbatim in a captured line in-tree; the
// captured lines live one-per-line in
// testdata/rate_limit_lines/<backend>.txt exactly as they appeared to
// the operator, and the directory README records each line's source.
// matchesBuiltinRateLimitLines anchors at the line start (after the
// family's optional prefix), so a captured line that leads with a phrase
// is detected while a line that merely quotes one is not. The claude-code
// usage-dialog entries of the former generic list (the warning, the
// reset-time announcement and the stop-and-wait menu option) are part of
// the claude-code table: each is anchored to a line that dialog prints.
// The generic seeds that no in-tree capture attributes to any backend
// (the plain "rate limit exceeded" / "quota exceeded" wordings) are no
// longer built in; operators who still rely on them list them under
// detection.rate_limit_patterns and get them back through the
// flat-substring path.
//
// Known gaps
// ----------
// Backends other than claude-code do not yet have a captured throttle
// line in testdata/rate_limit_lines/, so their entries are empty: only
// the shared protocol marker fires for them. When an operator reports a
// real line for one, add it to the fixture (README: source) and to the
// table — the flat-fingerprint test pins the backend association.

package session

import (
	"sort"
	"strings"
)

// builtinRateLimitMarker is the shared explicit protocol entry: any
// backend may emit it and it is recognized line-start only, after
// trimming.
const builtinRateLimitMarker = "DATAWATCH_RATE_LIMITED:"

// builtinRateLimitMarkerLower is the lowercase form used for comparison.
const builtinRateLimitMarkerLower = "datawatch_rate_limited:"

// builtinRateLimitPhrases is the built-in leading phrases per backend
// family, lowercased for the case-insensitive anchored match. Entries
// are nil until the first captured line lands in
// testdata/rate_limit_lines/<family>.txt. Every phrase must appear
// verbatim (case-insensitively) in the recorded lines of that fixture,
// and every fixture line must be detected by the anchored matcher —
// both directions are enforced by TestBuiltinRateLimitFixtureDetection:
// the fixture is the source of truth, not this table.
var builtinRateLimitPhrases = map[string][]string{
	"claude-code": {
		"you've hit your limit", // claude-code.txt: "⎿  You've hit your limit · resets 10pm (America/New_York)" and the v5.27.4 release-note capture
		"5-hour limit reached",  // claude-code.txt: "5-hour limit reached ∙ resets 2pm"
		"weekly limit reached",  // claude-code.txt: "Weekly limit reached"
		"hit weekly limit",      // claude-code.txt: "Hit weekly limit"
		"opus limit reached",    // claude-code.txt: "Opus limit reached"
		"sonnet limit reached",  // claude-code.txt: "Sonnet limit reached"
		// "You're out of extra usage · resets 11:50am (America/New_York)"
		// (claude-code.txt line 7) carries both of these, verbatim;
		// the wider catch was itself recorded in-tree when it was
		// first added: RELEASE-NOTES-v6.11.3.md:24 ("wider catch for
		// related 'you're out of credits/quota/usage' phrasings").
		"out of extra usage",
		"you're out of",
		// The claude-code usage dialog. The WARN tier and the HARD-limit
		// dialog print these as their own lines (fixtures: claude-code.txt
		// lines 8-14); the menu option is anchored through the family's
		// menu-row prefixes below.
		"approaching usage limit",
		"your usage limit will reset",
		"usage limit will reset",
		"limit will reset at",
		"weekly usage limit",
		"stop and wait",
		"wait for limit to reset",
		"resets at",
	},
	// No captured lines yet — the shared protocol marker is the only
	// built-in detection for these backends.
	"pi-prompt":       nil,
	"opencode":        nil,
	"opencode-acp":    nil,
	"opencode-prompt": nil,
	"ollama":          nil,
}

// builtinRateLimitLinePrefixes holds the family's own recorded leading
// glyphs for its prompt-dialog lines, byte-exact: the output gutter
// (⎿ U+237F) and the numbered menu rows of the usage dialog, where the
// selected row carries the ❯ (U+276F) cursor. A phrase after one of these
// prefixes counts as line-leading for that family only.
var builtinRateLimitLinePrefixes = map[string][]string{
	"claude-code": {"⎿  ", "❯ 1. ", "1. "},
}

// matchesBuiltinRateLimitLines reports whether line is a built-in
// rate-limit signal for backendFamily under the rules documented in the
// package comment: marker line-start (every family) or anchored phrase
// line-start (this family only), compared case-insensitively after
// trimming surrounding whitespace.
func matchesBuiltinRateLimitLines(backendFamily, line string) bool {
	low := strings.ToLower(strings.TrimSpace(line))
	if len(low) == 0 {
		return false
	}
	if strings.HasPrefix(low, builtinRateLimitMarkerLower) {
		return true
	}
	for _, phrase := range builtinRateLimitPhrases[backendFamily] {
		if strings.HasPrefix(low, phrase) {
			return true
		}
		for _, prefix := range builtinRateLimitLinePrefixes[backendFamily] {
			if strings.HasPrefix(low, strings.ToLower(prefix)+phrase) {
				return true
			}
		}
	}
	return false
}

// builtinRateLimitPatternsFlat flattens the built-in table for the
// patterns_version fingerprint. Each entry encodes the backend family it
// belongs to ("<family>|<phrase>", family prefix entries as
// "<family>|prefix|<glyph>") so the sort+dedup in writePatterns cannot
// erase the association: reassigning a phrase from one backend to
// another changes the set and therefore the digest. The shared marker is
// included once (it is shared, so it needs no family qualifier).
func builtinRateLimitPatternsFlat() []string {
	out := []string{builtinRateLimitMarker}
	families := make([]string, 0, len(builtinRateLimitPhrases))
	for family := range builtinRateLimitPhrases {
		families = append(families, family)
	}
	sort.Strings(families)
	for _, family := range families {
		for _, phrase := range builtinRateLimitPhrases[family] {
			out = append(out, family+"|"+phrase)
		}
		for _, prefix := range builtinRateLimitLinePrefixes[family] {
			out = append(out, family+"|prefix|"+prefix)
		}
	}
	return out
}
