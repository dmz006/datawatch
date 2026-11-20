// decompose_lint_spec_text.go — the check_matches_spec_text rule:
// the verification script (and its followed helpers, one level deep)
// must only assert what the task's own spec asks for. A check that
// looks for a markdown heading (a quoted literal of one to six #
// followed by whitespace, as quoted or as a sed address) or the exact
// gate output format "Final answer" / "Verification summary" without
// the task spec naming that heading or format — verbatim, or in
// equivalent wording — is flagged: the assertion is anchored to the
// spec, never to an assumption about output shape. A script that does
// not exist yet is skipped, exactly like the other
// verification-script rules.
package autonomous

import (
	"path/filepath"
	"regexp"
	"strings"
)

// specTextPhrases is the gate output format a spec must state for a
// script that checks it: the exact phrases, or equivalent wording that
// states the same format.
var specTextPhrases = []string{"Final answer", "Verification summary"}

// phraseStopWords are words that carry no content of their own and are
// dropped before a checked phrase and the spec are compared, so a
// phrase that differs only in filler words from what the spec states
// is anchored by it. The same set is used for the
// verify_script_text_not_in_spec rule through phraseMeaningMatches.
var phraseStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "for": true, "in": true, "is": true,
	"of": true, "or": true, "the": true, "to": true, "with": true,
}

// phraseTrimChars: edge punctuation, quoting and markdown-marker
// characters that are stripped from both ends of each word before
// comparison — quotes, brackets, hashes, slashes and the rest, in both
// ASCII and curly forms. A word made entirely of them (a bare ##) does
// not count as content.
const phraseTrimChars = "\t`'\"“”‘’;:,().!?[]{}<>|/\\-=+*^~#"

// phraseContentWords returns s's content words: whitespace tokens,
// lowercased, edge punctuation and markdown markers trimmed off, with
// the stop words dropped. A string with no content words returns nil —
// it can never anchor or fire a finding.
func phraseContentWords(s string) []string {
	out := []string{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, phraseTrimChars)
		if w == "" || phraseStopWords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// stemCandidates returns the fold variants of one content word: the
// word itself plus the bases left after stripping simple inflection
// endings (-ing, -ed, -es, -s; each only when it actually ends in the
// ending and the base stays at least two letters) and, recursively,
// the silent-e base (write -> writ, gates -> gat). Two words are
// considered the same stem when their candidate sets intersect, so
// pass/passed, test/tests, gate/gates and write/writing all compare
// equal while unrelated words never collapse.
func stemCandidates(w string) map[string]bool {
	out := map[string]bool{w: true}
	add := func(s string) {
		if len(s) >= 2 {
			out[s] = true
		}
	}
	for _, sfx := range []string{"ing", "ed", "es", "s"} {
		if len(w) > len(sfx)+1 && strings.HasSuffix(w, sfx) {
			add(w[:len(w)-len(sfx)])
		}
	}
	for s := range out {
		if len(s) > 3 && strings.HasSuffix(s, "e") {
			add(s[:len(s)-1])
		}
	}
	return out
}

// stemWordsEqual reports whether two content words compare equal after
// the simple plural/verb inflection folds.
func stemWordsEqual(a, b string) bool {
	ca := stemCandidates(a)
	for v := range stemCandidates(b) {
		if ca[v] {
			return true
		}
	}
	return false
}

// phraseMeaningMatches reports whether the spec states the phrase in
// meaning — word order and surrounding wording aside: every content
// word of the phrase (after lowercasing, stripping punctuation and
// markdown markers, dropping the stop words) occurs among the spec's
// content words under the simple plural/verb inflection folds
// (stemWordsEqual). A phrase with no content words never matches.
func phraseMeaningMatches(spec, phrase string) bool {
	words := phraseContentWords(phrase)
	if len(words) == 0 {
		return false
	}
	specWords := phraseContentWords(spec)
	for _, w := range words {
		matched := false
		for _, sw := range specWords {
			if stemWordsEqual(w, sw) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

var (
	// specTextCheckLineRe marks a line that performs a text check: a
	// grep-family, sed, awk, case or test invocation, or an [ / [[ test.
	// Only quoted literals on such lines are candidates — a literal on
	// any other line (an echo, an assignment) is not a check.
	specTextCheckLineRe = regexp.MustCompile(`\b(?:grep|egrep|fgrep|sed|awk|case|test)\b|\[\[|(^|;|\|)\s*\[`)
	// specTextSingleRe / specTextDoubleRe: one quoted literal (no
	// newline, no matching quote inside). Backreferences are not
	// available (RE2), so single- and double-quoted strings are
	// collected separately.
	specTextSingleRe = regexp.MustCompile(`'([^'\n]*)'`)
	specTextDoubleRe = regexp.MustCompile(`"([^"\n]*)"`)
	// specTextHeadingRe: a markdown heading — one to six # followed by
	// whitespace and non-empty text.
	specTextHeadingRe = regexp.MustCompile(`^#{1,6}[ \t]+\S`)
)

// specTextFile is one script under inspection: repo-relative path and
// content, in reading order (the declared script first, then its
// sourced helpers in the order it sources them).
type specTextFile struct {
	path    string
	content string
}

// specTextScriptFiles returns the declared script and its one-level-deep
// sourced helpers in reading order, or ok=false when there is nothing
// to read: no script declared, a command string, no directory, a
// missing file or a path outside the directory.
func specTextScriptFiles(dir string, t Task) ([]specTextFile, bool) {
	script := strings.TrimSpace(t.VerificationScript)
	if script == "" || dir == "" || verificationScriptLooksLikeCommand(script) {
		return nil, false
	}
	main, ok := readProjectFile(dir, script)
	if !ok {
		return nil, false
	}
	files := []specTextFile{{path: script, content: main}}
	seen := map[string]bool{filepath.Clean(script): true}
	for _, m := range verifySourceRe.FindAllStringSubmatch(main, -1) {
		rel := filepath.Clean(m[1])
		if seen[rel] || filepath.IsAbs(rel) {
			continue
		}
		seen[rel] = true
		if helper, ok := readProjectFile(dir, rel); ok {
			files = append(files, specTextFile{path: rel, content: helper})
		}
	}
	return files, true
}

// specTextCandidatesOnLine returns the check items a line asserts:
// quoted literals that are a markdown heading (as quoted, or as a sed
// address like `/## Gates/`) or one of the exact gate phrases.
// Arbitrary words never match.
func specTextCandidatesOnLine(line string) []string {
	if !specTextCheckLineRe.MatchString(line) {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	lits := append(specTextSingleRe.FindAllStringSubmatch(line, -1), specTextDoubleRe.FindAllStringSubmatch(line, -1)...)
	for _, lit := range lits {
		s := lit[1]
		if seen[s] || s == "" {
			continue
		}
		seen[s] = true
		if specTextHeadingRe.MatchString(s) ||
			// sed address form: /pattern[/flags] (e.g. sed -n '/## Gates/p').
			specTextHeadingRe.MatchString(strings.TrimSuffix(strings.TrimPrefix(s, "/"), "/")) ||
			isSpecTextPhrase(s) {
			out = append(out, s)
		}
	}
	return out
}

func isSpecTextPhrase(s string) bool {
	for _, p := range specTextPhrases {
		if s == p {
			return true
		}
	}
	return false
}

// specAnchorsItem reports whether the spec names the checked item —
// verbatim, or in equivalent wording (see phraseMeaningMatches). A
// heading item keeps the existing leading-# stripping: a spec that
// says "the Gates section" anchors a script that greps "## Gates", a
// spec that names the heading with # anchors a script that greps it
// without, and a spec that merely states the gate's purpose in other
// words ("run the gates in the report") anchors both.
func specAnchorsItem(spec, item string) bool {
	item = strings.TrimSpace(item)
	if item == "" {
		return false
	}
	if strings.Contains(spec, item) {
		return true
	}
	if s := strings.TrimLeft(item, "#\t"); s != "" && s != item {
		if strings.Contains(spec, s) {
			return true
		}
		return phraseMeaningMatches(spec, s)
	}
	return phraseMeaningMatches(spec, item)
}

// verifyScriptSpecUnanchored returns the first check item the declared
// script — or one of its one-level-deep sourced helpers — asserts that
// the task's own spec does not name, with the repo-relative script that
// contains it. ok is false when there is nothing to read (a script that
// does not exist yet is skipped, never guessed) or every check item is
// anchored in the spec.
func verifyScriptSpecUnanchored(dir string, t Task) (item, script string, ok bool) {
	files, ok := specTextScriptFiles(dir, t)
	if !ok {
		return "", "", false
	}
	seen := map[string]bool{}
	for _, f := range files {
		lines := strings.Split(f.content, "\n")
		for i := range lines {
			for _, c := range specTextCandidatesOnLine(lines[i]) {
				if seen[c] {
					continue
				}
				seen[c] = true
				if !specAnchorsItem(t.Spec, c) {
					return c, f.path, true
				}
			}
		}
	}
	return "", "", false
}
