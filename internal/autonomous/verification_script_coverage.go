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
// response and rejects it when it plans real file changes but names a
// verification script nowhere. Two conditions must both hold before this
// fires, matching the exact scope verificationScriptSuffix itself used:
//
//   - projectDir != "" — the same gate the prompt suffix itself uses. A
//     headless/ask-only backend was never asked for scripts (it has
//     nowhere to write one), so it isn't held to this bar either.
//   - at least one story or task plans real file changes (FilesPlanned
//     non-empty). A PRD whose decompose output has no files planned
//     anywhere (pure research, or paths genuinely unknown ahead of
//     time, same tolerance the schema's own "files" field already
//     documents) is not held to this bar.
func validateVerificationScriptCoverage(projectDir string, stories []Story) error {
	if projectDir == "" {
		return nil
	}
	var plansFiles, hasScript bool
	taskCount := 0
	for _, s := range stories {
		if len(s.FilesPlanned) > 0 {
			plansFiles = true
		}
		if strings.TrimSpace(s.VerificationScript) != "" {
			hasScript = true
		}
		for _, t := range s.Tasks {
			taskCount++
			if len(t.FilesPlanned) > 0 {
				plansFiles = true
			}
			if strings.TrimSpace(t.VerificationScript) != "" {
				hasScript = true
			}
		}
	}
	if plansFiles && !hasScript {
		return fmt.Errorf(
			"decompose produced zero verification_script values across %d stories / %d tasks despite planning real file changes — a tooled PRD requires at least one deterministic check. Retry decompose; if it recurs, check the planning backend/model actually has file access and is following the schema's verification_script field",
			len(stories), taskCount,
		)
	}
	return nil
}
