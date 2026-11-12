package autonomous

import "regexp"

// smokeTaskTitleRe matches a task title that is a live-smoke test or a
// final verification gate, case-insensitively, on word boundaries:
// "smoke", "final gate", or "final-gate" — but never embedded words like
// "smokescreen". The optional [ -]? covers the spelled and hyphenated
// variants of "final gate".
var smokeTaskTitleRe = regexp.MustCompile(`(?i)\b(?:smoke|final[ -]?gate)\b`)

// isSmokeVerificationTarget reports whether task is a smoke-verification
// target: tagged by the decomposer as a smoke or final-gate task
// (Task.Kind), or titled as one (title regex above).
func isSmokeVerificationTarget(task Task) bool {
	if task.Kind == TaskKindSmoke || task.Kind == TaskKindFinalGate {
		return true
	}
	return smokeTaskTitleRe.MatchString(task.Title)
}

// ApplySmokeVerificationOverrides sets the task-level verification
// LLM override (Task.VerificationBackend / Task.VerificationModel —
// the same fields SetTaskVerificationLLM writes, so
// ResolveVerificationLLM sees them as a task-level source) for every
// smoke/final-gate task in prd, from the autonomous-level
// smoke_verification_backend / smoke_verification_model config.
//
// Never-clobber rule: a task slot is only filled when it is currently
// empty. This is deliberate: an explicitly operator-set override
// (set-task-verification-llm, including a deliberate "" clear) is the
// most specific level in the chain and must survive decompose-time
// bulk application — the config fields are a default surface, not an
// override of overrides. Setting a slot only when empty also makes the
// function idempotent.
//
// backend and model are independent: backend set + model empty leaves
// the model slot to the existing explicit value or the story/PRD/global
// chain; model set + backend empty sets the model on a task that
// inherits its backend (resolution keeps the more specific model over a
// less specific backend). When both are empty the function is a no-op.
//
// Pure: mutates only prd's story/task fields; no store access, no IO.
func ApplySmokeVerificationOverrides(prd *PRD, backend, model string) {
	if prd == nil || (backend == "" && model == "") {
		return
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			task := &prd.Story[si].Tasks[ti]
			if !isSmokeVerificationTarget(*task) {
				continue
			}
			if backend != "" && task.VerificationBackend == "" {
				task.VerificationBackend = backend
			}
			if model != "" && task.VerificationModel == "" {
				task.VerificationModel = model
			}
		}
	}
}
