package autonomous

import (
	"sort"
	"strings"
	"unicode"
)

// MinSharedTerms is the minimum number of shared distinct terms a pair of
// texts must have before it can match, regardless of its overlap score.
const MinSharedTerms = 3

// displaySharedLimit caps how many shared terms are returned for display.
const displaySharedLimit = 5

// overlapStopwords are common English words dropped from term sets so that
// overlap is measured on meaningful content words only.
var overlapStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "or": {}, "but": {}, "not": {},
	"for": {}, "with": {}, "from": {}, "into": {}, "onto": {},
	"over": {}, "under": {}, "this": {}, "that": {}, "these": {},
	"those": {}, "they": {}, "them": {}, "their": {}, "his": {},
	"her": {}, "its": {}, "our": {}, "your": {}, "who": {},
	"which": {}, "what": {}, "when": {}, "where": {}, "how": {},
	"why": {}, "can": {}, "will": {}, "would": {}, "should": {},
	"could": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "the": {},
}

// TermOverlapResult describes how two texts overlap on distinct terms.
type TermOverlapResult struct {
	// Score is the count of shared distinct terms divided by the size of
	// the smaller term set, and zero when either term set is empty.
	Score float64
	// Matched reports whether the pair meets both the score threshold and
	// the minimum shared terms requirement.
	Matched bool
	// Shared lists shared terms, sorted and truncated for display.
	Shared []string
}

// normalizeOverlapTerms lower-cases text, splits it on any rune that is not
// a letter or digit, and returns the distinct tokens that are at least three
// characters long and not on the stopword list.
func normalizeOverlapTerms(text string) map[string]struct{} {
	terms := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(token)) < 3 {
			continue
		}
		if _, dropped := overlapStopwords[token]; dropped {
			continue
		}
		terms[token] = struct{}{}
	}
	return terms
}

// sharedOverlapTerms returns the terms present in both sets, sorted.
func sharedOverlapTerms(left, right map[string]struct{}) []string {
	shared := make([]string, 0, len(left))
	for term := range left {
		if _, ok := right[term]; ok {
			shared = append(shared, term)
		}
	}
	sort.Strings(shared)
	return shared
}

// MatchTermOverlap scores two texts on shared distinct terms. A pair matches
// when its score is at least threshold and its shared count is at least
// MinSharedTerms. The matcher is pure: no I/O, no store access, no model call.
func MatchTermOverlap(threshold float64, left, right string) TermOverlapResult {
	leftTerms := normalizeOverlapTerms(left)
	rightTerms := normalizeOverlapTerms(right)
	if len(leftTerms) == 0 || len(rightTerms) == 0 {
		return TermOverlapResult{}
	}
	shared := sharedOverlapTerms(leftTerms, rightTerms)
	score := float64(len(shared)) / float64(min(len(leftTerms), len(rightTerms)))
	display := shared
	if len(display) > displaySharedLimit {
		display = display[:displaySharedLimit]
	}
	return TermOverlapResult{
		Score:   score,
		Matched: score >= threshold && len(shared) >= MinSharedTerms,
		Shared:  display,
	}
}
