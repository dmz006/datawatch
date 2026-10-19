package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// verdictForAlreadyLanded handles a retry (priorAttempts > 0) of a task with a
// non-empty VerificationScript whose per-attempt diff window can be empty by
// construction — an earlier attempt of the same task may already have landed
// the real work. It runs the task's own deterministic verification script
// exactly once against the project dir and returns a handled verdict (the
// second return value is true) for three outcomes:
//
//  1. script exits 0 — the work clearly landed already: a pass verdict
//     (Severity "low", VerifiedAt set) that additionally sets UnmodifiedRetry
//     so the auditable record shows the empty-diff verdict was overridden by
//     the deterministic script, not the LLM judge.
//  2. script exists but exits non-zero — a handled FAILURE (OK=false): no new
//     changes were detected AND the task's own verification says the work is
//     not done. The summary states the exit code and the (runner-truncated)
//     script output is carried in Summary/Issues so the executor's failure
//     hint shows exactly why, instead of the generic "task produced no
//     output" text.
//  3. script missing (errors.Is(err, ErrVerificationScriptMissing)) — a setup
//     fault, explicitly distinct from an "empty output" judgment: OK=false,
//     Category "setup", and a summary naming the missing script's path so an
//     operator sees exactly which file to restore.
//
// Every other case — first attempt (priorAttempts <= 0), empty or whitespace
// script, or an unexpected runner error — returns (zero, false) without a
// verdict, so the caller falls through to the existing no-output failure
// exactly as before; a caller that only checks (result, bool) still sees
// "true" only when a verdict is authoritative, with the pass verdict's shape
// unchanged apart from the new UnmodifiedRetry flag.
func verdictForAlreadyLanded(scriptRel string, projectDir string, priorAttempts int) (autonomouspkg.VerificationResult, bool) {
	if priorAttempts <= 0 {
		return autonomouspkg.VerificationResult{}, false
	}
	if strings.TrimSpace(scriptRel) == "" {
		return autonomouspkg.VerificationResult{}, false
	}
	output, exitCode, runErr := autonomouspkg.RunVerificationScriptExit(scriptRel, projectDir)
	if runErr != nil {
		if errors.Is(runErr, autonomouspkg.ErrVerificationScriptMissing) {
			return autonomouspkg.VerificationResult{
				OK:       false,
				Severity: "medium",
				Category: "setup",
				Summary: fmt.Sprintf(
					"verifier: SETUP FAULT — verification script %q is missing from the worktree; restore that file before re-verifying. This is a missing-script setup fault, NOT a 'task produced no output' judgment",
					scriptRel),
				Issues:     []string{fmt.Sprintf("missing verification script: %s (%v)", scriptRel, runErr)},
				VerifiedAt: time.Now(),
			}, true
		}
		// Unexpected runner errors (unsafe path, failed bash launch):
		// unchanged behavior — fall through to the existing no-output failure.
		return autonomouspkg.VerificationResult{}, false
	}
	if exitCode != 0 {
		return autonomouspkg.VerificationResult{
			OK:       false,
			Severity: "medium",
			Summary: fmt.Sprintf(
				"verifier: no new changes were detected, and the task's own verification script %q failed with exit code %d (work did not land). Script output follows in issues.",
				scriptRel, exitCode),
			Issues:     scriptFailureIssues(scriptRel, exitCode, output),
			VerifiedAt: time.Now(),
		}, true
	}
	return autonomouspkg.VerificationResult{
		OK:         true,
		Severity:   "low",
		Summary:    "verifier: no new changes; work already landed in an earlier attempt and its verification script passed",
		VerifiedAt: time.Now(),
		// v9.2.1 — audit flag: this pass rested on the re-run verification
		// script, not on a diff or the LLM judge.
		UnmodifiedRetry: true,
	}, true
}

// scriptFailureIssues builds the Issues list for a retry where the task's own
// verification script exists but failed: the exit code plus the
// (runner-truncated) combined output, so the executor's failure hint carries
// the real reason instead of the generic no-output text.
func scriptFailureIssues(scriptRel string, exitCode int, output string) []string {
	issues := []string{
		fmt.Sprintf("verification script %q exited %d (no new changes to verify after retry)", scriptRel, exitCode),
	}
	if strings.TrimSpace(output) != "" {
		issues = append(issues, "script output:\n"+output)
	}
	return issues
}
