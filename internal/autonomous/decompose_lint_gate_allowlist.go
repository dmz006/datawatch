// decompose_lint_gate_allowlist.go — gate-allowlist coverage inputs for
// the decompose-lint verify_script_allowed_files rule: the file paths a
// plan's task specs and titles name (which a final gate's literal
// allowed-file list must cover even when the task's file lists omit
// them) and the sibling test files an in-tree Go change needs.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
)

// gateAllowlistSpecPaths returns the repo-relative file paths named in
// each task's spec and title, keyed by path with the owner text that
// names the naming task ("the spec of task <title>"). A gate script
// that omits one of these paths rejects a path the plan itself names,
// so the allowlist is incomplete no matter what the task's file lists
// say.
func gateAllowlistSpecPaths(stories []Story) map[string]string {
	owner := map[string]string{}
	for _, s := range stories {
		for _, t := range s.Tasks {
			for _, p := range specPathTokens(t.Title + " " + t.Spec) {
				if _, ok := owner[p]; !ok {
					owner[p] = "the spec of task " + t.Title
				}
			}
		}
	}
	return owner
}

// specPathTokens extracts the literal repo-relative file path tokens a
// piece of spec text names: tokens with a slash or a known file
// extension (preflightPathExts), split on whitespace and quotes,
// trailing punctuation trimmed. URLs (a scheme "://"), absolute or
// home-relative paths and shell-substituted tokens (a dollar sign)
// are not literal paths the plan names, so they are skipped.
func specPathTokens(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '"' || r == '\'' ||
			r == '(' || r == ')' || r == ';' || r == '|' || r == '&' || r == '`' ||
			r == '=' || r == ',' || r == '<' || r == '>'
	}) {
		tok := strings.Trim(field, ".,;:!?\"'`()<>[]")
		if tok == "" || strings.Contains(tok, "$") || strings.Contains(tok, "://") {
			continue
		}
		tok = strings.TrimPrefix(tok, "./")
		if tok == "" || strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "~") {
			continue
		}
		isPath := strings.Contains(tok, "/")
		for _, ext := range preflightPathExts {
			if strings.HasSuffix(tok, ext) {
				isPath = true
			}
		}
		if !isPath || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// preflightSiblingTestFile returns the sibling _test.go path for an
// in-tree Go source file: empty when rel is not a .go source file,
// when dir is unavailable, when the source file is not a regular file
// under dir, or when its sibling test file does not exist on disk.
func preflightSiblingTestFile(dir, rel string) string {
	if dir == "" || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return ""
	}
	src := filepath.Join(dir, rel)
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		return ""
	}
	sib := strings.TrimSuffix(rel, ".go") + "_test.go"
	if st, err := os.Stat(filepath.Join(dir, sib)); err != nil || st.IsDir() {
		return ""
	}
	return sib
}
