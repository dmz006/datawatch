// B124 (2026-10-12) — kick-back gate. BL416 Phase 2 asked a tooled
// decompose session to write a real verification_script per unit (see
// verification_script_prompt.go), but nothing ever checked the model
// actually did it, so a response that silently skipped the field shipped
// through to needs_review unnoticed. Found live: two real PRDs decomposed
// by a real tooled session after Phase 2 shipped produced zero
// verification_script values across 16 tasks between them — traced to
// PlanningPromptSession's own schema block never listing the field at
// all (fixed alongside this gate), so the model was correctly following
// the schema it was told to follow "exactly." This gate exists so that
// failure mode, and any other cause of the same outcome, can never again
// ship through decompose unnoticed: it rejects outright, the same way an
// unparseable response already does, forcing a retry rather than
// silently landing in needs_review with a plan nothing will ever
// mechanically check.
package autonomous

import (
	"fmt"
	"strings"
)

// validateVerificationScriptCoverage inspects a freshly-parsed decompose
// response and rejects it when any file-changing unit leaves its
// verification script unnamed — per unit, not per plan. A story-level
// script does NOT excuse its tasks: a single whole-plan script can cover
// exactly one unit, and a worker running the other task would have
// nothing of its own to check with. Rules:
//
//   - projectDir == "" — the same gate the prompt suffix itself uses. A
//     headless/ask-only backend was never asked for scripts (it has
//     nowhere to write one), so it isn't held to this bar either.
//   - every task with FilesPlanned non-empty must carry its own
//     non-blank VerificationScript. A task with no planned files
//     (pure research, or paths genuinely unknown ahead of time, same
//     tolerance the schema's own "files" field already documents) is
//     exempt.
//   - a story with FilesPlanned non-empty and zero tasks must carry its
//     own non-blank VerificationScript.
//
// Every offender is collected (not just the first) so a retry attempt
// sees the full list in one error.
func validateVerificationScriptCoverage(projectDir string, stories []Story) error {
	if projectDir == "" {
		return nil
	}
	var offenders []string
	for _, s := range stories {
		if len(s.FilesPlanned) > 0 && len(s.Tasks) == 0 && strings.TrimSpace(s.VerificationScript) == "" {
			offenders = append(offenders, fmt.Sprintf("story %q", s.Title))
		}
		for _, t := range s.Tasks {
			if len(t.FilesPlanned) > 0 && strings.TrimSpace(t.VerificationScript) == "" {
				offenders = append(offenders, fmt.Sprintf("story %q / task %q", s.Title, t.Title))
			}
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	return fmt.Errorf(
		"decompose left file-changing units with no verification_script: %s. A story-level script does not excuse its tasks, and a task with no planned files is exempt — every file-changing unit needs its own deterministic check. Retry decompose; if it recurs, check the planning backend/model actually has file access and is following the schema's verification_script field. Give every file-changing task a verification_script path under .datawatch/verify/ that checks only that task's own files",
		strings.Join(offenders, "; "),
	)
}
