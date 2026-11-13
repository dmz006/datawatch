// decompose_lint_verify_script.go — the script-content checks behind the
// verify_script_task_files_only and verify_script_docs_without_sync
// decompose-lint rules, and the task-kind / reason-empty predicates of
// final_gate_without_script. A verification script is read from the
// project directory; a script that does not exist yet (or is a command
// string, or lies outside the directory) is skipped here — the
// unexecutable_verification_script rule keeps covering it.
package autonomous

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxVerifyScriptBytes bounds how much of a script or helper is read.
const maxVerifyScriptBytes = 1 << 20

var (
	// verifySourceRe finds a `. file` / `source file` line.
	verifySourceRe = regexp.MustCompile(`(?m)^\s*(?:\.|source)\s+["']?([^\s"';&|$` + "`" + `]+)["']?`)
	// verifyMergeBaseRe: the merge base, by command or by three-dot range.
	verifyMergeBaseRe = regexp.MustCompile(`merge-base|\b(?:main|master)\.\.\.`)
	// verifyFileCheckRe: a file-listing comparison of the tree.
	verifyFileCheckRe = regexp.MustCompile(`git\s+(?:diff|status|ls-files|diff-tree)\b[^\n]*(?:--name-only|--name-status|--porcelain|--short|--stat|\s-s\b)|git\s+ls-files\b`)
	// verifyDocsRe: a docs / embedded-docs / how-to coverage check.
	verifyDocsRe = regexp.MustCompile(`check-docs-sync|check-howto-coverage|internal/server/web/docs|docs/howto`)
	// verifySyncDocsRe: the docs sync is run.
	verifySyncDocsRe = regexp.MustCompile(`\bmake\b[^\n]*\bsync-docs\b`)
)

// verifyScriptContent returns the repo-relative script path and its text
// followed by the text of every helper it sources (one level deep). ok is
// false when there is nothing to read: no script, a command string, no
// directory, a missing file or a path outside the directory.
func verifyScriptContent(dir string, t Task) (path, content string, ok bool) {
	script := strings.TrimSpace(t.VerificationScript)
	if script == "" || dir == "" || verificationScriptLooksLikeCommand(script) {
		return "", "", false
	}
	main, ok := readProjectFile(dir, script)
	if !ok {
		return "", "", false
	}
	var b strings.Builder
	b.WriteString(main)
	seen := map[string]bool{filepath.Clean(script): true}
	for _, m := range verifySourceRe.FindAllStringSubmatch(main, -1) {
		rel := filepath.Clean(m[1])
		if seen[rel] || filepath.IsAbs(rel) {
			continue
		}
		seen[rel] = true
		if helper, ok := readProjectFile(dir, rel); ok {
			b.WriteString("\n")
			b.WriteString(helper)
		}
	}
	return script, b.String(), true
}

// readProjectFile reads a repo-relative file under dir, refusing paths
// that escape it.
func readProjectFile(dir, rel string) (string, bool) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	f, err := os.Open(filepath.Join(dir, clean))
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, maxVerifyScriptBytes)
	n, _ := f.Read(buf)
	return string(buf[:n]), true
}

// scriptListsOwnFiles reports whether content names every file the task
// plans (a literal allowed-file list equal to the task's own files).
func scriptListsOwnFiles(content string, t Task) bool {
	if len(t.FilesPlanned) == 0 {
		return false
	}
	for _, f := range t.FilesPlanned {
		if f = strings.TrimSpace(f); f == "" || !strings.Contains(content, f) {
			return false
		}
	}
	return true
}

// verifyScriptTaskFilesOnly returns the script path when the script
// compares only against the task's own files (a literal list equal to
// them, or a file-listing diff/status of the tree) and never against the
// merge base.
func verifyScriptTaskFilesOnly(dir string, t Task) (string, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok || verifyMergeBaseRe.MatchString(content) {
		return "", false
	}
	if verifyFileCheckRe.MatchString(content) || (scriptListsOwnFiles(content, t) && strings.Contains(content, "diff")) {
		return path, true
	}
	return "", false
}

// verifyScriptDocsWithoutSync returns the script path when the script
// checks docs, the embedded docs copy or how-to coverage without running
// `make sync-docs`.
func verifyScriptDocsWithoutSync(dir string, t Task) (string, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok || !verifyDocsRe.MatchString(content) || verifySyncDocsRe.MatchString(content) {
		return "", false
	}
	return path, true
}

// taskHasVerificationReason reports whether the task states why it has no
// verification script.
func taskHasVerificationReason(t Task) bool {
	return strings.TrimSpace(t.VerificationScriptReason) != ""
}

// verifyScriptFindingPath returns the verification script path a finding
// for m on t should report: the script the content predicates read, else
// the task's declared script (empty when none).
func verifyScriptFindingPath(m *compiledMatch, dir string, t Task) string {
	if m.taskFilesOnly {
		if p, ok := verifyScriptTaskFilesOnly(dir, t); ok {
			return p
		}
	}
	if m.docsNoSync {
		if p, ok := verifyScriptDocsWithoutSync(dir, t); ok {
			return p
		}
	}
	for _, sub := range m.anyOf {
		if p := verifyScriptFindingPath(&sub, dir, t); p != "" {
			return p
		}
	}
	for _, sub := range m.allOf {
		if p := verifyScriptFindingPath(&sub, dir, t); p != "" {
			return p
		}
	}
	return strings.TrimSpace(t.VerificationScript)
}
