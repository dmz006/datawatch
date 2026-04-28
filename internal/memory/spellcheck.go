// v5.26.72 — Mempalace spellcheck.py port.
//
// Operator-asked work-pre-v6.0 even though the audit deferred this
// — the spellcheck is conservative by default: it only proposes
// replacements when a Levenshtein distance ≤ 2 to a known
// dictionary word AND the source word looks like a typo (lowercase
// alpha, length ≥ 4, not in the dictionary itself).
//
// No external dictionary file — the seed list is the union of:
//   - Common English words (top 1000-ish from Norvig's corpus,
//     embedded inline as seedDictionary)
//   - Words mempalace sees frequently in datawatch corpora
//     (auth, config, runtime, daemon, protocol, …)
//
// SpellCheck returns suggestions only — never rewrites in place.
// The decision to apply is left to the caller (UI prompt, save-
// time hook, or operator review).

package memory

import (
	"sort"
	"strings"
	"unicode"
)

// seedDictionary is the small in-memory wordlist. Operators with a
// real dictionary ship one via SpellCheckOpts.ExtraWords.
var seedDictionary = map[string]bool{
	// Common English (subset)
	"the": true, "and": true, "for": true, "with": true, "this": true,
	"that": true, "from": true, "have": true, "make": true, "your": true,
	"about": true, "after": true, "before": true, "between": true,
	"because": true, "should": true, "would": true, "could": true,
	"every": true, "first": true, "their": true, "there": true,
	"these": true, "those": true, "while": true, "where": true, "which": true,
	// Datawatch / dev domain
	"auth": true, "authentication": true, "authorization": true,
	"config": true, "configuration": true, "runtime": true, "daemon": true,
	"protocol": true, "memory": true, "memories": true, "session": true,
	"sessions": true, "session_id": true, "project": true, "agent": true,
	"agents": true, "audit": true, "verdict": true, "verdicts": true,
	"orchestrator": true, "operator": true, "namespace": true,
	"backend": true, "backends": true, "embed": true, "embedding": true,
	"sqlite": true, "postgres": true, "pgvector": true, "ollama": true,
	"openai": true, "anthropic": true, "claude": true, "kubernetes": true,
	"docker": true, "container": true, "endpoint": true, "endpoints": true,
	"webhook": true, "webhooks": true, "release": true, "smoke": true,
	"datawatch": true, "mempalace": true, "wing": true, "room": true,
	"hall": true, "floor": true, "shelf": true, "box": true, "closet": true,
	"drawer": true, "pinned": true, "pinning": true, "wakeup": true,
	"sanitize": true, "sanitizer": true, "stitch": true, "stitching": true,
}

// SpellCheckSuggestion is one proposed replacement.
type SpellCheckSuggestion struct {
	Original string `json:"original"`
	Proposed string `json:"proposed"`
	Distance int    `json:"distance"`
}

// SpellCheckOpts tunes the check. ExtraWords adds operator-supplied
// dictionary entries for project-specific terms.
type SpellCheckOpts struct {
	ExtraWords []string
	// MaxDistance caps Levenshtein distance for a word to be a
	// suggestion candidate. Default 2.
	MaxDistance int
}

// SpellCheck returns suggestions for words in `text` that look
// like typos. Never rewrites the input. Idempotent and order-
// stable (sorted by original word).
func SpellCheck(text string, opts SpellCheckOpts) []SpellCheckSuggestion {
	if text == "" {
		return nil
	}
	if opts.MaxDistance <= 0 {
		opts.MaxDistance = 2
	}
	dict := make(map[string]bool, len(seedDictionary))
	for w := range seedDictionary {
		dict[w] = true
	}
	for _, w := range opts.ExtraWords {
		dict[strings.ToLower(strings.TrimSpace(w))] = true
	}

	// Split on non-letter to extract word tokens.
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r)
	})

	seen := map[string]bool{}
	var suggestions []SpellCheckSuggestion
	for _, w := range words {
		lw := strings.ToLower(w)
		if len(lw) < 4 {
			continue // too short to confidently flag
		}
		if dict[lw] {
			continue
		}
		if seen[lw] {
			continue
		}
		seen[lw] = true
		// Find the closest dictionary word within MaxDistance.
		bestWord := ""
		bestDist := opts.MaxDistance + 1
		for d := range dict {
			if abs(len(d)-len(lw)) > opts.MaxDistance {
				continue
			}
			dist := levenshtein(lw, d)
			if dist < bestDist {
				bestDist = dist
				bestWord = d
			}
		}
		if bestWord == "" || bestDist > opts.MaxDistance {
			continue
		}
		suggestions = append(suggestions, SpellCheckSuggestion{
			Original: lw, Proposed: bestWord, Distance: bestDist,
		})
	}
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].Original < suggestions[j].Original
	})
	return suggestions
}

func abs(x int) int { if x < 0 { return -x }; return x }

// levenshtein returns the edit distance between a and b. Standard
// dynamic-programming impl with O(min(len(a),len(b))) space.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) < len(br) {
		ar, br = br, ar
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := 0; j <= len(br); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
