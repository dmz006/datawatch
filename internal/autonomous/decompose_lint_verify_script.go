// decompose_lint_verify_script.go — the script-content checks behind the
// verify_script_task_files_only, verify_script_docs_without_sync,
// artefact_files_allowed and final_gate_artefact_allowed decompose-lint
// rules, plus the task-kind /
// reason-empty predicates of final_gate_without_script. A verification
// script is read from the project directory; a script that does not
// exist yet (or is a command string, or lies outside the directory) is
// skipped here — the unexecutable_verification_script rule keeps covering
// it.
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
	// verifyArtefactVocabRe: one of the generated artefacts the daemon
	// writes per Automaton (changelog draft, parity draft, gate report);
	// a changed-file-set check that names any of these exempts them.
	verifyArtefactVocabRe = regexp.MustCompile(`docs/plans/drafts|docs/plans/parity-issues|docs/plans/reports|CHANGELOG`)
	// verifyFinalGateSentinelRe: the FINAL_GATE / DW_FINAL_GATE sentinel
	// (an environment variable or argument) a whole-file-set check may
	// key an early return on, so the final gate re-runs the script on
	// the cumulative tree with the check skipped.
	verifyFinalGateSentinelRe = regexp.MustCompile(`\b(?:DW_)?FINAL_GATE\b`)
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
	defer func() { _ = f.Close() }()
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

// verifyScriptArtefactFilesAllowed returns the script path when the
// script does a changed-file-set check (a git diff/status/ls-files
// listing of the tree) without exempting any of the generated artefacts
// (docs/plans/drafts, docs/plans/parity-issues, docs/plans/reports or
// CHANGELOG) the daemon writes per Automaton: the final gate re-runs
// the script on the cumulative tree where those files are present, so
// an unexempted file-set check can only fail there. A script with no
// file-set check never matches, and an unreadable script is skipped —
// as with the other script-content rules.
func verifyScriptArtefactFilesAllowed(dir string, t Task) (string, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok || !verifyFileCheckRe.MatchString(content) || verifyArtefactVocabRe.MatchString(content) {
		return "", false
	}
	return path, true
}

// verifyScriptFinalGateArtefactAllowed returns the script path when the
// script enforces the whole changed-file set against a finite list (a
// git diff/status/ls-files listing) and exempts the generated artefacts
// (docs/plans/drafts, docs/plans/parity-issues, docs/plans/reports or
// CHANGELOG), but carries no FINAL_GATE / DW_FINAL_GATE sentinel to
// skip the check when the final gate re-runs the script on the
// cumulative tree: earlier tasks' files are present there, so a finite
// list can only fail at the gate. The generated-artefact half is
// covered by artefact_files_allowed, so a script lacking those
// exemptions never matches here. A script with no whole-listing
// assertion never matches, and an unreadable script is skipped — as
// with the other script-content rules.
func verifyScriptFinalGateArtefactAllowed(dir string, t Task) (string, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok || !verifyFileCheckRe.MatchString(content) || !verifyArtefactVocabRe.MatchString(content) {
		return "", false
	}
	if verifyFinalGateSentinelRe.MatchString(content) {
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
	return verifyScriptFindingPathIn(m, dir, t, nil)
}

// verifyScriptFindingPathIn is verifyScriptFindingPath with the plan-wide
// view the cross-task script predicates need.
func verifyScriptFindingPathIn(m *compiledMatch, dir string, t Task, plan *lintPlan) string {
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
	if m.artefactFiles {
		if p, ok := verifyScriptArtefactFilesAllowed(dir, t); ok {
			return p
		}
	}
	if m.finalGateArtefact {
		if p, ok := verifyScriptFinalGateArtefactAllowed(dir, t); ok {
			return p
		}
	}
	if m.invokesUnknownFlag {
		if p, _, ok := verifyScriptInvokesUnknownFlag(dir, t); ok {
			return p
		}
	}
	if m.specUnanchored {
		if _, p, ok := verifyScriptSpecUnanchored(dir, t); ok {
			return p
		}
	}
	if m.branchArtefactFiles {
		if p, ok := verifyScriptArtefactFiles(dir, t); ok {
			return p
		}
	}
	if m.finalGateRerun {
		if p, ok := verifyScriptFinalGateRerun(dir, t); ok {
			return p
		}
	}
	if m.unknownFlag {
		if p, _, ok := verifyScriptUnknownFlag(dir, t); ok {
			return p
		}
	}
	if m.textNotInSpec {
		if p, _, ok := verifyScriptTextNotInSpec(dir, t); ok {
			return p
		}
	}
	for _, sp := range scriptPredicates {
		if m.scriptPreds&sp.id != 0 {
			if hit, ok := sp.check(dir, t, plan); ok {
				return hit.Path
			}
		}
	}
	for _, sub := range m.anyOf {
		if p := verifyScriptFindingPathIn(&sub, dir, t, plan); p != "" {
			return p
		}
	}
	for _, sub := range m.allOf {
		if p := verifyScriptFindingPathIn(&sub, dir, t, plan); p != "" {
			return p
		}
	}
	return strings.TrimSpace(t.VerificationScript)
}

var (
	// verifyUnexpectedRe: the script flags files outside an allow-list.
	verifyUnexpectedRe = regexp.MustCompile(`(?i)unexpected|not\s+allowed|outside\s+(?:the\s+)?(?:allow|task)|grep\s+-[a-z]*v|\bcomm\s+-[0-9]+|\bextra\s+files?|disallowed`)
	// verifyAllowListRe: a finite allow-list the changed set is held to.
	verifyAllowListRe = regexp.MustCompile(`(?i)allow(?:ed)?[_ -]?(?:list|files?)?\s*=|\ballowed\b|case\s+"?\$[^\n]*\bin\b`)
	// verifyArtefactExempt: every daemon-generated artefact path exempted.
	verifyArtefactExempt = []string{"docs/plans/drafts/", "docs/plans/parity-issues/", "docs/plans/reports/", "CHANGELOG.md"}
	// verifyFinalGateGuardRe: an env-var or argument guard that skips the
	// set check, or a check limited to files the task adds.
	verifyFinalGateGuardRe = regexp.MustCompile(`(?i)\$\{?(?:\d|@|#|[A-Z_]*FINAL[A-Z_]*|DATAWATCH_[A-Z_]+|SKIP_[A-Z_]+)\}?|--final|--diff-filter=[A-Za-z]*A|ls-files\s+(?:-\w+\s+)*(?:--others|-o)\b`)
)

// scriptAssertsChangedSet reports whether content computes a changed-file
// set and flags files outside it.
func scriptAssertsChangedSet(content string) bool {
	return verifyFileCheckRe.MatchString(content) && verifyUnexpectedRe.MatchString(content)
}

// verifyScriptArtefactFiles returns the script path when the script flags
// unexpected changed files without exempting the daemon-generated
// artefact folders and CHANGELOG.md.
func verifyScriptArtefactFiles(dir string, t Task) (string, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok || !scriptAssertsChangedSet(content) {
		return "", false
	}
	for _, p := range verifyArtefactExempt {
		if !strings.Contains(content, p) {
			return path, true
		}
	}
	return "", false
}

// verifyScriptFinalGateRerun returns the script path when the script holds
// the whole changed set to a finite allow-list with no guard for the final
// gate rerunning it on the cumulative tree.
func verifyScriptFinalGateRerun(dir string, t Task) (string, bool) {
	path, content, ok := verifyScriptContent(dir, t)
	if !ok || !scriptAssertsChangedSet(content) || !verifyAllowListRe.MatchString(content) || verifyFinalGateGuardRe.MatchString(content) {
		return "", false
	}
	return path, true
}
