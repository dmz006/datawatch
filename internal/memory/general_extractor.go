// v5.26.72 — Mempalace general_extractor.py port.
//
// Schema-free fact extraction. Datawatch has entity_detector for
// "who/where/when" entity tagging, but mempalace also runs a
// schema-free extractor that returns subject-predicate-object
// triples without a fixed entity ontology. Useful for KG
// pre-population from session output.
//
// Two modes:
//
//  1. Heuristic — regex/keyword patterns find SVO triples cheaply
//     for common shapes ("X is Y", "X depends on Y", "X uses Y").
//     No LLM needed; ~0ms per call. Good signal-to-noise for
//     declarative content.
//
//  2. LLM-backed — when the caller passes a Refiner-shaped
//     interface, the extractor delegates to it for free-form
//     content where the heuristics miss. Optional.
//
// The KG path consumes the resulting triples via existing
// AddTriple flow (kg.go). No new schema needed.

package memory

import (
	"context"
	"regexp"
	"strings"
)

// Triple is the subject-predicate-object shape returned by
// ExtractFacts. Confidence is 0..1; the heuristic path picks
// 0.5, LLM-backed picks whatever it returns or 0.7 by default.
type Triple struct {
	Subject    string  `json:"subject"`
	Predicate  string  `json:"predicate"`
	Object     string  `json:"object"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"` // "heuristic" or "llm"
}

// FactExtractor is the optional LLM interface. When supplied to
// ExtractFacts, the extractor falls back to it for content that
// the heuristics didn't match. Returns the LLM's triples directly.
type FactExtractor interface {
	ExtractTriples(ctx context.Context, text string) ([]Triple, error)
}

// Pre-compiled patterns for the heuristic path.
var (
	patternIsA      = regexp.MustCompile(`(?i)\b([A-Z][\w\.]*(?:\s+[A-Z][\w\.]*)?)\s+is\s+(?:a|an|the)\s+([\w\-\s]{3,40}?)\b`)
	patternDependsOn = regexp.MustCompile(`(?i)\b([\w\.\-]{3,40})\s+depends\s+on\s+([\w\.\-]{3,40})\b`)
	patternUses     = regexp.MustCompile(`(?i)\b([\w\.\-]{3,40})\s+uses\s+([\w\.\-]{3,40})\b`)
	patternImports  = regexp.MustCompile(`(?i)\b([\w\.\-]{3,40})\s+imports\s+([\w\.\-]{3,40})\b`)
	patternCalls    = regexp.MustCompile(`(?i)\b([\w\.\-]{3,40})\s+calls\s+([\w\.\-]{3,40})\b`)
)

// ExtractFacts pulls subject-predicate-object triples from the
// supplied text. Heuristics run first; if the LLM extractor is
// non-nil and the heuristics produced fewer than 2 triples, the
// LLM path runs as fallback.
//
// Returns deduped triples (no exact subject+predicate+object
// repeats within the same call).
func ExtractFacts(ctx context.Context, text string, llm FactExtractor) ([]Triple, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var triples []Triple
	seen := map[string]bool{}

	push := func(subject, predicate, object, source string, conf float64) {
		s := strings.TrimSpace(subject)
		p := strings.TrimSpace(predicate)
		o := strings.TrimSpace(object)
		if s == "" || p == "" || o == "" {
			return
		}
		key := s + "|" + p + "|" + o
		if seen[key] {
			return
		}
		seen[key] = true
		triples = append(triples, Triple{
			Subject: s, Predicate: p, Object: o,
			Confidence: conf, Source: source,
		})
	}

	for _, m := range patternIsA.FindAllStringSubmatch(text, -1) {
		push(m[1], "is_a", m[2], "heuristic", 0.5)
	}
	for _, m := range patternDependsOn.FindAllStringSubmatch(text, -1) {
		push(m[1], "depends_on", m[2], "heuristic", 0.6)
	}
	for _, m := range patternUses.FindAllStringSubmatch(text, -1) {
		push(m[1], "uses", m[2], "heuristic", 0.5)
	}
	for _, m := range patternImports.FindAllStringSubmatch(text, -1) {
		push(m[1], "imports", m[2], "heuristic", 0.7)
	}
	for _, m := range patternCalls.FindAllStringSubmatch(text, -1) {
		push(m[1], "calls", m[2], "heuristic", 0.6)
	}

	if len(triples) < 2 && llm != nil {
		llmTriples, err := llm.ExtractTriples(ctx, text)
		if err != nil {
			// Heuristic results stay; LLM error is non-fatal.
			return triples, nil
		}
		for _, t := range llmTriples {
			if t.Confidence == 0 {
				t.Confidence = 0.7
			}
			if t.Source == "" {
				t.Source = "llm"
			}
			push(t.Subject, t.Predicate, t.Object, t.Source, t.Confidence)
		}
	}
	return triples, nil
}
