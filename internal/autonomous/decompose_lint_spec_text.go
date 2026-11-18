// decompose_lint_spec_text.go — the check_matches_spec_text rule:
// the verification script (and its followed helpers, one level deep)
// must only assert what the task's own spec asks for. A check that
// looks for a markdown heading (a quoted literal of one to six #
// followed by whitespace, as quoted or as a sed address) or the exact
// gate output format "Final answer" / "Verification summary" without
// the task spec naming that heading or format verbatim is flagged: the
// assertion is anchored to the spec, never to an assumption about
// output shape. A script that does not exist yet is skipped, exactly
// like the other verification-script rules.
package autonomous

import (
	"path/filepath"
	"regexp"
	"strings"
)

// specTextPhrases is the gate output format a spec must name verbatim
// for a script that checks it: the exact phrases, no variants.
var specTextPhrases = []string{"Final answer", "Verification summary"}

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

// specAnchorsItem reports whether the spec names the checked item:
// verbatim, or — for a heading item carrying # — with the leading #
// stripped, so a spec that says "the Gates section" anchors a script
// that greps "## Gates" (and a spec that names "## Gates" anchors a
// script that greps "Gates").
func specAnchorsItem(spec, item string) bool {
	item = strings.TrimSpace(item)
	if item == "" {
		return false
	}
	if strings.Contains(spec, item) {
		return true
	}
	if s := strings.TrimLeft(item, "#\t"); s != "" && s != item {
		return strings.Contains(spec, s)
	}
	return false
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
