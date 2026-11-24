// Plan-path lint: a lightweight, non-blocking check that runs alongside
// LintPlanScope at decompose-store time. LintPlanScope catches plans that
// reference paths OUTSIDE the PRD's allowed directories; this file catches
// the other class of planning mistake — paths that are inside the tree but
// point at directories that do not exist, usually a typo'd sibling of a
// real directory (e.g. "cmd/dftwatch" where "cmd/datch" was meant). A plan
// naming such a path is a near-certain broken spec, and the operator is
// better told at review time (needs_review) than left to watch the task
// worker fail on the first write.
//
// Like LintPlanScope, the result is a plain []string of warnings appended
// to PRD.ScopeWarnings: it never changes the decompose outcome, the PRD
// status, or retry behavior. Both decompose store paths
// (Decompose and decomposeStreamingCore) append the warnings before
// SetStories, so they surface through prd-get, REST
// GET /api/autonomous/prds/{id}, and the PWA warnings area with no new
// web work — the existing scope_warnings field already renders there.
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Two or more slash-separated segments of safe path characters.
	// Deliberately excludes backslashes and ':' so Windows paths and
	// drive letters never match, and ':' keeps URL schemes out of the
	// class (URLs are additionally masked before the scan).
	planPathTokenRe = regexp.MustCompile(`[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+`)
	// scheme://anything (up to the next whitespace) — masked out before
	// token extraction so "https://example.com/path" does not yield the
	// false candidate "example.com/path".
	planPathURLRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://\S*`)
	// $VAR / ${VAR} shell references — masked so "$ROOT/cmd/x" does not
	// yield the false candidate "ROOT/cmd/x".
	planPathVarRe = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_:-]*\}?`)
)

// lintPlanPathCandidate normalizes a raw value (a FilesPlanned entry or a
// spec/description token) into a clean repo-relative path, reporting ok =
// false for anything that is not a plausible repo-relative path: absolute
// paths (/usr, /bin, /home, /tmp), Windows paths (C:\..., C:/...),
// dot-relative refs (./..., ../...), URLs, and shell variables are all
// out of scope.
func lintPlanPathCandidate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\n\r") {
		return "", false
	}
	if strings.HasPrefix(s, "/") {
		return "", false // absolute unix path
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return "", false // working-directory-relative, not repo-relative
	}
	if strings.ContainsRune(s, '\\') {
		return "", false // windows-style
	}
	if len(s) >= 2 && s[1] == ':' {
		return "", false // drive-letter path (C:/...)
	}
	return filepath.Clean(s), true
}

// extractPlanPaths pulls path-like repo-relative tokens out of free text
// (a task Spec or story Description). Tokens are whitespace-delimited
// stretches of two or more slash-separated segments; URLs and shell
// variables are masked first, and tokens preceded by '/', '.' (dot-
// relative), ':' (drive letter), or '$' (variable) are rejected.
func extractPlanPaths(text string) []string {
	if text == "" {
		return nil
	}
	masked := planPathURLRe.ReplaceAllString(text, " ")
	masked = planPathVarRe.ReplaceAllString(masked, " ")
	seen := map[string]bool{}
	var out []string
	for _, m := range planPathTokenRe.FindAllStringIndex(masked, -1) {
		if m[0] > 0 {
			switch masked[m[0]-1] {
			case '/', '.', ':', '$', '"', '\'':
				continue // part of an absolute, dot-relative, drive, or var ref
			}
		}
		tok := strings.TrimRight(masked[m[0]:m[1]], ".") // drop sentence punctuation
		if !strings.ContainsRune(tok, '/') {
			continue
		}
		if _, ok := lintPlanPathCandidate(tok); !ok || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// LintPlanPaths returns one plain warning per planned path whose target
// AND whose parent directory are missing under PRD.ProjectDir. It is pure
// and non-blocking: callers append the result to the PRD's existing plan
// warnings; it must never affect the decompose outcome, PRD status, or
// retry behavior.
//
// Candidates come from every story/task FilesPlanned entry plus path-like
// tokens in each task Spec and story Description. Per unit (story or
// task), a candidate is skipped when:
//
//   - PRD.ProjectDir is empty or not an existing directory (nothing to
//     check against),
//   - it is listed in that unit's FilesGenerated (it creates the path),
//   - the path itself already exists, or
//   - only the final file is missing and its parent directory exists
//     (a new file in an existing directory is normal planning intent).
//
// Identical warnings are deduplicated.
func LintPlanPaths(prd *PRD, stories []Story) []string {
	if prd == nil || prd.ProjectDir == "" {
		return nil
	}
	root := filepath.Clean(prd.ProjectDir)
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	suppressedIn := func(gen []string, p string) bool {
		for _, g := range gen {
			if strings.TrimSpace(g) == p {
				return true
			}
		}
		return false
	}
	gather := func(list []string, text string, gen []string) []string {
		seenP := map[string]bool{}
		var cands []string
		for _, raw := range append(append([]string{}, list...), extractPlanPaths(text)...) {
			p, ok := lintPlanPathCandidate(raw)
			if !ok || suppressedIn(gen, p) || seenP[p] {
				continue
			}
			seenP[p] = true
			cands = append(cands, p)
		}
		return cands
	}
	emit := func(label, p string) {
		full := filepath.Join(root, p)
		if _, statErr := os.Stat(full); statErr == nil {
			return // candidate path exists — nothing to check
		}
		parentRel := filepath.Dir(p) // repo-relative parent ("." for top level)
		if _, perr := os.Stat(filepath.Join(root, parentRel)); perr == nil {
			return // parent exists (incl. the tree root) — a new file in an
			// existing directory is normal planning intent
		}
		// Both the path and its parent are missing → likely a typo'd dir.
		msg := fmt.Sprintf("plan path: %s names %q but parent directory %q does not exist under the PRD working tree — check the spec (possible typo)",
			label, p, parentRel)
		if seen[msg] {
			return
		}
		seen[msg] = true
		out = append(out, msg)
	}
	for _, s := range stories {
		label := fmt.Sprintf("story %q", s.Title)
		for _, p := range gather(s.FilesPlanned, s.Description, s.FilesGenerated) {
			emit(label, p)
		}
		for _, t := range s.Tasks {
			label = fmt.Sprintf("task %q", t.Title)
			for _, p := range gather(t.FilesPlanned, t.Spec, t.FilesGenerated) {
				emit(label, p)
			}
		}
	}
	return out
}
