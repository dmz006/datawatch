package session

import (
	"strings"
	"unicode/utf8"
)

const completionSummaryMarker = "DATAWATCH_COMPLETE:"

// completionSummaryPlaceholder is the instruction-template text that task
// prompts echo into the pane (see StartScreenCapture, which already skips it
// during live detection). Lines carrying it are not real summaries.
const completionSummaryPlaceholder = `<one-sentence summary`

// completionSummaryMaxLen caps the length of an extracted summary.
const completionSummaryMaxLen = 200

// completionBoxPrefixes are leading box/bullet characters the TUI may place
// in front of the marker, alongside any spaces.
const completionBoxPrefixes = "┃│⎿●>-* \t"

// ExtractCompletionSummary returns the last DATAWATCH_COMPLETE: summary found
// in raw session output text. It strips ANSI escapes (treating bare \r as a
// line boundary), scans lines from the end toward the start, and trims
// whitespace and leading box/bullet runes from each candidate before
// matching. Candidates that are empty or are the
// instruction placeholder are skipped; the first accepted one is collapsed to
// a single line and truncated to completionSummaryMaxLen characters.
// Returns "" if no valid summary is found.
func ExtractCompletionSummary(text string) string {
	clean := StripANSI(text)
	// Bare carriage returns (e.g. from in-line overdraw) also act as
	// line boundaries.
	clean = strings.ReplaceAll(clean, "\r", "\n")

	lines := strings.Split(clean, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, completionBoxPrefixes)
		line = strings.TrimSpace(line)

		if !strings.HasPrefix(line, completionSummaryMarker) {
			continue
		}
		summary := strings.TrimSpace(strings.TrimPrefix(line, completionSummaryMarker))
		if summary == "" {
			continue
		}
		if strings.Contains(summary, completionSummaryPlaceholder) {
			continue
		}
		summary = strings.Join(strings.Fields(summary), " ")
		return truncateRune(summary, completionSummaryMaxLen)
	}
	return ""
}

// truncateRune shortens s to at most n runes, if it is longer.
func truncateRune(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
