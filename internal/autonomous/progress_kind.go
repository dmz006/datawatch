package autonomous

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kinds an Automaton can carry. An empty PRD.Kind means "derive at read time".
const (
	PRDKindFeature = "feature"
	PRDKindBug     = "bug"
	PRDKindOther   = "other"
)

// NormalizePRDKind trims and lower-cases s. ok is true for the three kinds and
// for the empty string (which means "not set").
func NormalizePRDKind(s string) (string, bool) {
	k := strings.ToLower(strings.TrimSpace(s))
	switch k {
	case "", PRDKindFeature, PRDKindBug, PRDKindOther:
		return k, true
	}
	return k, false
}

// bugWords is the word list of the keyword heuristic that the status script
// uses to tell bug work from feature work. Each entry is a case-insensitive
// regexp that must match as a whole word.
var bugWords = compileAnchoredAlternatives(
	"fix", "flak", "defect", "bug", "root.cause", "stall", "regress", "race", "leak",
	"must not", "no longer", "crash", "broken", "fails", "fail", "failure", "blocker",
	"suppress", "cve", "trivy", "deterministic", "isolate", "refuse", "stuck",
)

// bugTextRunes is how many characters of the spec feed the heuristic.
const bugTextRunes = 240

// DeriveKind guesses the kind from the title and the start of the spec: "bug"
// when the text holds a bug keyword as a whole word, else "feature".
func DeriveKind(title, spec string) string {
	if containsWholeWord(title+" "+firstRunes(spec, bugTextRunes), bugWords) {
		return PRDKindBug
	}
	return PRDKindFeature
}

// EffectiveKind resolves the kind of an Automaton. A valid explicit p.Kind wins,
// then the kind of the backlog row that links the record (rowKinds, keyed by
// record id), then DeriveKind.
func EffectiveKind(p *PRD, rowKinds map[string]string) string {
	if p == nil {
		return PRDKindFeature
	}
	if k, ok := NormalizePRDKind(p.Kind); ok && k != "" {
		return k
	}
	if k, ok := NormalizePRDKind(rowKinds[p.ID]); ok && k != "" {
		return k
	}
	return DeriveKind(p.Title, p.Spec)
}

// firstRunes returns at most n characters of s (not bytes).
func firstRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// compileAnchoredAlternatives builds one case-insensitive regexp per
// alternative, anchored at the start of the text it is matched against.
func compileAnchoredAlternatives(alts ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(alts))
	for i, a := range alts {
		out[i] = regexp.MustCompile(`(?i)^(?:` + a + `)`)
	}
	return out
}

// isWordRune matches what a unicode-aware \w matches.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// containsWholeWord reports whether text holds a match of any alternative that
// starts and ends on a word boundary. Go's \b only knows ASCII, so the
// boundaries are checked here to treat non-ASCII letters as word characters.
func containsWholeWord(text string, alts []*regexp.Regexp) bool {
	prevWord := false
	for i, r := range text {
		if !prevWord {
			rest := text[i:]
			for _, re := range alts {
				loc := re.FindStringIndex(rest)
				if loc == nil {
					continue
				}
				next, _ := utf8.DecodeRuneInString(rest[loc[1]:])
				if loc[1] == len(rest) || !isWordRune(next) {
					return true
				}
			}
		}
		prevWord = isWordRune(r)
	}
	return false
}
