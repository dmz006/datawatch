package autonomous

import (
	"fmt"
	"strings"
)

// noRetryReason reports whether a failed task's next attempt should be
// skipped because it cannot plausibly change the outcome, and the
// operator-readable reason why. previous holds this task's earlier failure
// summaries in chronological order (oldest first, newest last); the summary
// comparison is insensitive to surrounding whitespace and case.
//
// ok is true in exactly three cases:
//
//  1. the verifier failed the task because no files changed, and the task
//     is marked as writing nothing — read-only or no planned files, the
//     same check taskAttemptKind uses to classify a task as a note. A
//     re-run of a task that writes nothing has no new work to verify.
//
//  2. the verification script the task names is missing from the worktree
//     — the same missing condition the script runner reports as
//     ErrVerificationScriptMissing, checked without executing the script.
//     The script is looked up in the directory the task resolves to via
//     ResolveTaskProjectDir; without a PRD in hand that is the task's own
//     ProjectDir or WorktreePath, and a task whose directory comes only
//     from the PRD's ProjectDir cannot be checked here — call
//     noRetryReasonWithPRD with the PRD so the lookup can see it.
//
//  3. the task has already failed twice and both of those failures carried
//     a summary identical to this one. A third identical failure means
//     retrying is not fixing anything.
//
// Every other combination returns ok=false: the task should be retried.
func noRetryReason(t *Task, v VerificationResult, previous []string) (reason string, ok bool) {
	return noRetryReasonWithPRD(nil, t, v, previous)
}

// noRetryReasonWithPRD is noRetryReason with the task's PRD in hand, so the
// missing-script lookup (case 2) can also see a project directory that
// comes from the PRD's ProjectDir rather than from the task's own override:
// the directory is resolved with ResolveTaskProjectDir(prd, t), where the
// task's own ProjectDir/WorktreePath still wins over the PRD's. prd may be
// nil, which degrades to exactly noRetryReason.
func noRetryReasonWithPRD(prd *PRD, t *Task, v VerificationResult, previous []string) (reason string, ok bool) {
	if !v.OK && verifierSaysNoFilesChanged(v) && taskWritesNothing(t) {
		return "verifier reports no files changed and the task writes nothing, so a re-run has no new work to verify", true
	}
	if t != nil {
		if script := strings.TrimSpace(t.VerificationScript); script != "" {
			if dir := ResolveTaskProjectDir(prd, t); dir != "" && verificationScriptMissing(script, dir) {
				return fmt.Sprintf("verification script %s is missing from the worktree", script), true
			}
		}
	}
	if sameFailureSummaryTwice(v.Summary, previous) {
		return "this failure summary has already failed twice; a third identical attempt will not change the outcome", true
	}
	return "", false
}

// taskWritesNothing is the note check taskAttemptKind uses: a nil task, a
// read-only task, or one with no planned files writes nothing to the tree.
func taskWritesNothing(t *Task) bool {
	if t == nil {
		return true
	}
	return t.ReadOnly || len(t.FilesPlanned) == 0
}

// noFilesChangedPhrases are the phrases a verifier's failure text carries
// when it failed the task because the attempt changed nothing.
var noFilesChangedPhrases = []string{
	"no files changed",
	"no file changes",
	"no changed files",
	"did not change any files",
}

// verifierSaysNoFilesChanged reports whether the verification result's
// failure text (summary plus issue lines) says the attempt changed no
// files. Matching is case-insensitive.
func verifierSaysNoFilesChanged(v VerificationResult) bool {
	text := strings.ToLower(v.Summary)
	for _, issue := range v.Issues {
		text += " " + strings.ToLower(issue)
	}
	for _, phrase := range noFilesChangedPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// normalizeFailureSummary trims surrounding whitespace and lowercases a
// failure summary before comparison.
func normalizeFailureSummary(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// sameFailureSummaryTwice reports whether the two most recent entries of
// previous (newest last) are both identical to summary after
// normalization. Fewer than two earlier summaries, or a blank summary,
// never count.
func sameFailureSummaryTwice(summary string, previous []string) bool {
	if len(previous) < 2 {
		return false
	}
	want := normalizeFailureSummary(summary)
	if want == "" {
		return false
	}
	n := len(previous)
	return normalizeFailureSummary(previous[n-1]) == want &&
		normalizeFailureSummary(previous[n-2]) == want
}
