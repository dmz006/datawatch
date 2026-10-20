// plan_coverage.go — pure, deterministic plan-requirement coverage
// checking. Given an operator's spec, it extracts the requirements the
// decomposed plan must satisfy (numbered items, obligation sentences,
// test mentions, named file paths / config keys) and then verdicts
// how well a parsed plan (story and task titles + specs) covers each
// one. Everything here is string-level and side-effect free: no LLM
// call, no I/O, no daemon — the same spec+plan always produces the
// same report, which is what lets callers store the report on the PRD
// and let later code act on it.
package autonomous

import (
	"regexp"
	"strings"
)

// CoverageVerdict classifies how well a decomposed plan meets one
// extracted spec requirement.
type CoverageVerdict string

// The three possible verdicts for a single requirement.
const (
	// CoverageCovered: every core term of the requirement appears in
	// the plan (un-negated), or the requirement text itself does.
	CoverageCovered CoverageVerdict = "covered"
	// CoverageMissing: no core term of the requirement appears in the
	// plan at all — the plan drops it entirely.
	CoverageMissing CoverageVerdict = "missing"
	// CoverageWeakened: at least one core term appears, but some are
	// absent or present only in a negated form — partially or
	// contradicted coverage.
	CoverageWeakened CoverageVerdict = "weakened"
)

// RequirementVerdict pairs an extracted requirement with its verdict
// and the spec fragment it came from (the requirement text itself).
type RequirementVerdict struct {
	Requirement string          // requirement text, as extracted from the spec
	Verdict     CoverageVerdict // covered / missing / weakened
}

// PlanCoverageReport summarizes how fully a decomposed plan covers the
// spec's extractable requirements. It is plain data: nothing in this
// package stores it — the caller (e.g. decompose wiring) attaches the
// report to the PRD.
type PlanCoverageReport struct {
	Covered   int                  // requirements fully covered
	Missing   int                  // requirements with no trace at all in the plan
	Weakened  int                  // requirements present but partial or negated
	Unmatched []RequirementVerdict // every non-covered requirement, in spec order
}

// Extraction and matching helpers. All of these work on lowercased text
// and are intentionally conservative: a spec that mentions nothing
// extractable yields zero requirements and therefore a zero-count
// report (rather than a failure).

var (
	// Numbered list items at the start of a line: "1. text" / "1) text"
	// / "(1) text".
	numberedItemRe = regexp.MustCompile(`(?i)^[ \t]*(?:\(([0-9]+)\)|([0-9]+)[\.\)])\s+`)
	// Obligation words that mark a sentence as a requirement.
	obligationRe = regexp.MustCompile(`(?i)\b(must|shall|should|required)\b`)
	// A sentence that mentions a test naming something.
	testMentionRe = regexp.MustCompile(`(?i)\b(tests?|test\s+cases?|test\s+suites?)\b`)
	// Named file paths and dotted config keys: any run of word
	// characters joined by '/', '.' or '-', with at least one separator
	// ("reports/daily.txt", "autonomous.planning_timeout_seconds").
	pathTokenRe = regexp.MustCompile(`[a-z0-9_-]+(?:[./-][a-z0-9_-]+)+`)
	// Bare snake_case keys: "planning_retry_count".
	snakeKeyRe = regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z][a-z0-9]*)+`)
	// Core-term tokenizer. Dotted / slashed names stay whole tokens.
	coverageTermRe = regexp.MustCompile(`[a-z0-9_]+(?:[./][a-z0-9_]+)*`)
	// Sentence splitter: a terminal followed by whitespace. Trailing
	// punctuation at end-of-line is left attached, so file names with
	// dots are not split apart.
	sentenceSplitRe = regexp.MustCompile(`[.!?]+\s+`)
)

// coverageStopwords are words that carry no requirement content. The
// obligation words are stopwords for *matching* (they triggered
// extraction; they should not be terms the plan has to repeat).
var coverageStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "at": true, "by": true, "for": true,
	"with": true, "as": true, "so": true, "via": true,
	"is": true, "are": true, "be": true, "been": true, "was": true, "were": true,
	"it": true, "its": true, "this": true, "that": true, "these": true,
	"those": true, "which": true, "who": true, "whom": true, "when": true,
	"where": true, "while": true, "each": true, "per": true, "any": true,
	"all": true, "into": true, "onto": true, "from": true, "such": true,
	"then": true, "than": true, "them": true, "their": true, "we": true,
	"you": true, "your": true, "our": true, "i": true, "e.g": true, "i.e": true,
	// obligation triggers — not matching terms:
	"must": true, "shall": true, "should": true, "required": true,
}

// negationWords mark a term occurrence as negated when they are the
// word(s) immediately preceding it ("not add tests", "does not flush").
var negationWords = map[string]bool{
	"no": true, "not": true, "never": true, "without": true, "nor": true,
	"cannot": true, "nobody": true, "nothing": true, "none": true,
	"don't": true, "doesn't": true, "didn't": true, "won't": true,
	"wouldn't": true, "couldn't": true, "can't": true, "shouldn't": true,
}

// coverageRequirementKey is the dedup key for a requirement: trimmed,
// lowercased, trailing sentence punctuation stripped so "Add tests"
// and "Add tests." are the same requirement.
func coverageRequirementKey(s string) string {
	k := strings.ToLower(strings.TrimSpace(s))
	k = strings.TrimRight(k, " \t.!?;:,")
	return k
}

// ExtractRequirements returns the ordered, deduplicated list of
// requirement strings in spec, per the extractable set:
//
//  1. numbered items ("1. text", "1) text", "(1) text") — their text,
//     numbering stripped;
//  2. sentences containing an obligation word (must / required / shall
//     / should, case-insensitive);
//  3. sentences mentioning a test that name something (test / tests /
//     test cases / test suites plus at least one non-stopword token);
//  4. every named file path, dotted config key or snake_case key token
//     appearing anywhere in the spec.
//
// A spec with none of these yields an empty list; that is not an
// error — the caller just gets a zero-count report.
func ExtractRequirements(spec string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(s string) {
		k := coverageRequirementKey(s)
		if len(k) < 3 || seen[k] {
			return
		}
		if coverageTermRe.FindStringIndex(k) == nil {
			return // no alphanumeric content at all — nothing to enforce
		}
		seen[k] = true
		out = append(out, strings.TrimSpace(s))
	}

	lines := strings.Split(spec, "\n")

	// 1. Numbered items keep line order.
	for _, line := range lines {
		if loc := numberedItemRe.FindStringIndex(line); loc != nil {
			add(line[loc[1]:])
		}
	}

	// 2 + 3. Sentence passes over the whole spec.
	for _, line := range lines {
		for _, sentence := range sentenceSplitRe.Split(line, -1) {
			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}
			if obligationRe.MatchString(sentence) {
				add(sentence)
				continue
			}
			if testMentionRe.MatchString(sentence) && testSentenceNamesSomething(sentence) {
				add(sentence)
			}
		}
	}

	// 4. Named paths / config keys, in order of appearance.
	low := strings.ToLower(spec)
	for _, tok := range pathTokenRe.FindAllString(low, -1) {
		if len(tok) >= 4 && !isPureNumeric(tok) {
			add(tok)
		}
	}
	for _, tok := range snakeKeyRe.FindAllString(low, -1) {
		if len(tok) >= 3 && !isPureNumeric(tok) {
			add(tok)
		}
	}
	return out
}

// testSentenceNamesSomething reports whether a test-mentioning sentence
// names at least one thing (one non-stopword, non-test token left over).
func testSentenceNamesSomething(sentence string) bool {
	for _, tok := range coverageTermRe.FindAllString(strings.ToLower(sentence), -1) {
		if !coverageStopwords[tok] && !testMentionRe.MatchString(tok) {
			return true
		}
	}
	return false
}

// isPureNumeric reports whether s is all digits (dots/dashes allowed).
func isPureNumeric(s string) bool {
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.' || r == '-':
			// separators are fine inside "30" / "5.0"
		default:
			return false
		}
	}
	return digits > 0
}

// CheckPlanCoverage extracts the requirements from spec and verdicts
// each one against the concatenated titles + specs (and story
// descriptions) of every story/task in the plan. It is pure and
// deterministic: the same spec+plan always yields the same report.
func CheckPlanCoverage(spec string, stories []Story) *PlanCoverageReport {
	planText := strings.ToLower(planCoverageText(stories))
	report := &PlanCoverageReport{Unmatched: []RequirementVerdict{}}
	for _, requirement := range ExtractRequirements(spec) {
		switch verdict := coverageVerdictFor(requirement, planText); verdict {
		case CoverageCovered:
			report.Covered++
		case CoverageMissing:
			report.Missing++
			report.Unmatched = append(report.Unmatched, RequirementVerdict{Requirement: requirement, Verdict: verdict})
		default:
			report.Weakened++
			report.Unmatched = append(report.Unmatched, RequirementVerdict{Requirement: requirement, Verdict: verdict})
		}
	}
	return report
}

// planCoverageText concatenates every story and task title+spec (plus
// story descriptions) into one haystack for term matching.
func planCoverageText(stories []Story) string {
	var b strings.Builder
	for _, s := range stories {
		b.WriteString(s.Title)
		b.WriteByte(' ')
		b.WriteString(s.Description)
		b.WriteByte(' ')
		for _, t := range s.Tasks {
			b.WriteString(t.Title)
			b.WriteByte(' ')
			b.WriteString(t.Spec)
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// coverageVerdictFor decides covered / missing / weakened for one
// requirement against the lowercased plan haystack:
//
//   - covered: the requirement text itself is in the plan, or every
//     core term is present and at least one occurrence of each term is
//     un-negated;
//   - weakened: at least one core term is present, but some are absent
//     or only present in negated form;
//   - missing: no core term is present at all.
//
// A requirement with no core terms (only stopwords) is vacuously
// covered: there is nothing to enforce.
func coverageVerdictFor(requirement, planLower string) CoverageVerdict {
	if strings.Contains(planLower, strings.ToLower(strings.TrimSpace(requirement))) {
		return CoverageCovered
	}
	terms := coverageTerms(requirement)
	if len(terms) == 0 {
		return CoverageCovered
	}
	presentAny := false
	allPresent := true
	allEffective := true
	for _, term := range terms {
		present, effective := termCoverage(planLower, term)
		if !present {
			allPresent = false
			continue
		}
		presentAny = true
		if !effective {
			allEffective = false
		}
	}
	if allPresent && allEffective {
		return CoverageCovered
	}
	if presentAny {
		return CoverageWeakened
	}
	return CoverageMissing
}

// coverageTerms returns the deduplicated core-term list for requirement:
// lowercased tokens (dotted/slashed names stay whole), stopwords and
// pure numerics stripped.
func coverageTerms(requirement string) []string {
	low := strings.ToLower(requirement)
	seen := make(map[string]bool)
	var terms []string
	for _, tok := range coverageTermRe.FindAllString(low, -1) {
		if coverageStopwords[tok] || isPureNumeric(tok) || seen[tok] {
			continue
		}
		seen[tok] = true
		terms = append(terms, tok)
	}
	return terms
}

// termCoverage reports whether term appears in planLower at a word
// boundary (present) and whether it appears at least once in an
// un-negated context (effective).
func termCoverage(planLower, term string) (present, effective bool) {
	for i := 0; ; {
		off := strings.Index(planLower[i:], term)
		if off < 0 {
			break
		}
		pos := i + off
		if termBoundaryOK(planLower, pos, len(term)) {
			present = true
			if !occurrenceNegated(planLower, pos) {
				effective = true
				return true, true
			}
		}
		i = pos + 1
	}
	return false, false
}

// termBoundaryOK reports whether the word at pos sits on token
// boundaries: the neighbors (if any) are not letters/digits/underscores.
func termBoundaryOK(s string, pos, termLen int) bool {
	if pos > 0 && isTokenChar(s[pos-1]) {
		return false
	}
	end := pos + termLen
	if end < len(s) && isTokenChar(s[end]) {
		return false
	}
	return true
}

func isTokenChar(r byte) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// occurrenceNegated reports whether the word immediately before pos in s
// is a negation word (covering "do not", "does not", … two-word forms).
func occurrenceNegated(s string, pos int) bool {
	start := pos - 16
	if start < 0 {
		start = 0
	}
	words := strings.Fields(s[start:pos])
	if len(words) == 0 {
		return false
	}
	last := words[len(words)-1]
	if negationWords[last] {
		return true
	}
	if len(words) >= 2 && negationWords[words[len(words)-2]] && last == "not" {
		return true
	}
	return false
}
