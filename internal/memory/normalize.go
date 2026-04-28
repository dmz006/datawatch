// v5.26.72 — Mempalace dialect.py + normalize.py port.
//
// Pre-save text normalization. Runs at the SaveWithNamespace
// boundary so every persisted memory shares a canonical shape
// regardless of which channel sourced it (voice transcript,
// Slack export, paste from a doc, agent stdout). Reduces
// false negatives in dedup + spurious mismatches in
// vector-similarity from punctuation drift.
//
// Pure Go — uses golang.org/x/text/unicode/norm for NFC.

package memory

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// fancyQuotePairs maps "smart" punctuation to its ASCII equivalent.
// The big sources of drift in operator-pasted content are auto-
// formatting from chat clients (Slack/Discord), word-processor
// quotes, and emoji-style ellipses.
var fancyQuotePairs = []struct{ from, to string }{
	{"‘", "'"}, // ‘
	{"’", "'"}, // ’
	{"“", `"`}, // “
	{"”", `"`}, // ”
	{"–", "-"}, // –
	{"—", "-"}, // —
	{"…", "..."},
	{" ", " "}, // NBSP
}

// Normalize returns the canonical-form representation of text.
// Order: NFC-normalize → fold fancy punctuation → collapse
// runs of whitespace → trim. Idempotent — calling Normalize
// on the result is a no-op.
func Normalize(text string) string {
	if text == "" {
		return ""
	}
	s := norm.NFC.String(text)
	for _, p := range fancyQuotePairs {
		s = strings.ReplaceAll(s, p.from, p.to)
	}
	// Collapse internal whitespace to single spaces but preserve
	// newlines (paragraph structure carries semantic weight).
	var out strings.Builder
	out.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' {
			out.WriteRune(r)
			prevSpace = false
			continue
		}
		if unicode.IsSpace(r) {
			if !prevSpace {
				out.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		out.WriteRune(r)
		prevSpace = false
	}
	// Trim trailing/leading whitespace per line.
	lines := strings.Split(out.String(), "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
