// Guardrail judge model resolution (v9.10.18).
//
// Every guardrail judge session (rules-review, ci-watch, per-task and
// per-story guardrails, rejudge) MUST run on a model that was
// configured, at an explicit level, by the operator. The old resolution
// (resolveGuardrailBackendModel, deleted with this file's introduction)
// returned an empty model whenever nothing was pinned and left the
// fallback to the execution path — so a pi-family judge launched with
// no override silently fell back to the backend's built-in default
// model, which evicts the operator's pinned model on the node.
//
// Resolution here is pure and paired: each candidate tier is accepted
// only if the resolved guardrail backend can serve it (validateModelForBackend
// — the v8.37.2 pairing rule built on servesProviderModels /
// carriesForeignBackendPrefix), so a provider-prefixed model is never
// paired with an incompatible backend and a bare Claude alias is never
// paired with a pi-family one. When no tier resolves, the typed error
// ErrNoGuardrailModel is returned and the caller records a loud,
// non-retried infra-category block verdict (noGuardrailModelVerdict)
// instead of ever launching a judge.
package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNoGuardrailModel signals that no guardrail model tier (PRD → task →
// global guardrail/verification/planning model) resolved to a model the
// guardrail backend can serve. Callers test for it with errors.Is and
// must fail loudly — a guardrail judge must never run on the
// backend's built-in default model.
var ErrNoGuardrailModel = errors.New("no guardrail judge model configured: none of the PRD guardrail_model, task guardrail_model, autonomous.guardrail_model, autonomous.verification_model or autonomous.planning_model tiers resolved to a model the guardrail backend can serve — a pi-family judge must never fall back to the backend's built-in default model")

// ResolveGuardrailModel resolves the model a guardrail judge run must use.
//
// Precedence tiers, first match wins:
//
//  1. prdModel   — the PRD's own guardrail_model pin (PRD.GuardrailModel)
//  2. taskModel  — the task-level guardrail_model override (Task.GuardrailModel)
//  3. globalGuardrailModel     — Config.GuardrailModel (autonomous.guardrail_model)
//  4. globalVerificationModel  — Config.VerificationModel (autonomous.verification_model)
//  5. globalPlanningModel      — Config.PlanningModel (autonomous.planning_model)
//
// Tiers 3–5 are the GLOBAL defaults, configured for the global pi-family
// backend; on a claude-code-resolved backend they are skipped
// (the exact leak BL418 found live: a flat pi-model name reaching a
// session-routed judge that rejects it) and only the explicit PRD/task
// pins (tiers 1–2) may resolve. claude-code non-regression: on the
// claude-code backend a bare Claude alias is accepted (tier 1/2), the
// global pi tiers never apply, and nothing configured anywhere yields
// ErrNoGuardrailModel — never a backend default.
//
// Every accepted tier's candidate is paired with the resolved guardrail
// backend (the backend parameter) via validateModelForBackend — the
// v8.37.2 rule built on servesProviderModels / carriesForeignBackendPrefix:
// a model written "<prefix>/<model>" for a foreign provider is skipped
// on a backend that cannot serve it (and a bare Claude alias is skipped
// on pi-family backends), so the function never returns a model the
// judge would reject.
//
// An empty backend parameter pairs as "any" (matching
// validateModelForBackend's own contract), but a non-empty backend
// enforces the pairing at every tier.
//
// When no tier resolves, ("", an error wrapping ErrNoGuardrailModel) is
// returned — the error names the resolved backend and every skipped
// candidate. Callers must then record a loud, non-retried
// infra-category block (noGuardrailModelVerdict) and must NOT launch a
// judge with an empty model.
func ResolveGuardrailModel(backend, prdModel, taskModel, globalGuardrailModel, globalVerificationModel, globalPlanningModel string) (string, error) {
	explicitTiers := []struct {
		label string
		model string
	}{
		{"PRD guardrail_model", prdModel},
		{"task guardrail_model", taskModel},
	}
	globalTiers := []struct {
		label string
		model string
	}{
		{"autonomous.guardrail_model", globalGuardrailModel},
		{"autonomous.verification_model", globalVerificationModel},
		{"autonomous.planning_model", globalPlanningModel},
	}

	claudeCode := backend == claudeCodeBackendName
	if claudeCode {
		// Tiers 3–5 are global pi-family defaults; on a claude-code
		// resolved backend they are pi-specific and must not leak into a
		// session-routed judge (BL418). They are recorded as skipped
		// below for the error message only.
		globalTiers = nil
	}

	skipped := make([]string, 0, len(explicitTiers)+len(globalTiers))
	for _, tier := range explicitTiers {
		model := strings.TrimSpace(tier.model)
		if model == "" {
			continue
		}
		if err := validateModelForBackend(backend, model); err != nil {
			skipped = append(skipped, tier.label+" "+model)
			continue
		}
		return model, nil
	}
	for _, tier := range globalTiers {
		model := strings.TrimSpace(tier.model)
		if model == "" {
			continue
		}
		if err := validateModelForBackend(backend, model); err != nil {
			skipped = append(skipped, tier.label+" "+model)
			continue
		}
		return model, nil
	}
	if claudeCode {
		for _, tier := range []struct {
			label string
			model string
		}{
			{"autonomous.guardrail_model", globalGuardrailModel},
			{"autonomous.verification_model", globalVerificationModel},
			{"autonomous.planning_model", globalPlanningModel},
		} {
			if m := strings.TrimSpace(tier.model); m != "" {
				skipped = append(skipped, tier.label+" "+m+" (pi-specific global, skipped on the claude-code backend)")
			}
		}
	}
	detail := ""
	if len(skipped) > 0 {
		detail = "; skipped, cannot be served: " + strings.Join(skipped, ", ")
	}
	return "", fmt.Errorf("%w (resolved backend %q)%s", ErrNoGuardrailModel, backend, detail)
}

// resolveUnitGuardrailModelLocked is the pure core of guardrail
// model resolution for one guardrail invocation: it resolves the
// effective backend (task → PRD → cfg.VerificationBackend, unchanged
// from the BL416 Phase 1 rule) and the model (ResolveGuardrailModel's
// precedence tiers over the cfg snapshot's global fields).
//
// It is lock-free by design: callers that already hold Manager.mu
// (e.g. SetTaskGuardrailLLM) pass their already-loaded cfg snapshot;
// the Manager method below acquires the lock itself.
func resolveUnitGuardrailModelLocked(cfg Config, t *Task, prd *PRD) (backend, model string, err error) {
	if t == nil {
		t = &Task{}
	}
	if prd == nil {
		prd = &PRD{}
	}
	switch {
	case t.GuardrailBackend != "":
		backend = t.GuardrailBackend
	case prd.GuardrailBackend != "":
		backend = prd.GuardrailBackend
	default:
		backend = cfg.VerificationBackend
	}
	model, err = ResolveGuardrailModel(
		backend,
		prd.GuardrailModel,
		t.GuardrailModel,
		cfg.GuardrailModel,
		cfg.VerificationModel,
		cfg.PlanningModel,
	)
	return backend, model, err
}

// resolveUnitGuardrailModel resolves the effective guardrail backend and
// model for a guardrail invocation on prd (and t, when a task-level
// guardrail runs), reading the manager's current global config under its
// own lock. It returns an error wrapping ErrNoGuardrailModel when no
// tier resolves a model the backend can serve; callers must then record
// a loud, non-retried infra-category block instead of launching a judge.
func (m *Manager) resolveUnitGuardrailModel(t *Task, prd *PRD) (backend, model string, err error) {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	return resolveUnitGuardrailModelLocked(cfg, t, prd)
}

// noGuardrailModelVerdict builds the loud, NON-RETRIED infra-category
// block verdict a guardrail produces when its judge model cannot be
// resolved at any level. It is constructed directly — never passed
// through runGuardrailWithInfraRetry — so it is never retried: no
// amount of retrying configures a model that is not configured, and
// retrying would only burn budget and node state. Recorded as a plain
// block (Outcome "block", Category "infra") it stops the unit with a
// reason that names the configuration failure and the operator
// remedy.
func noGuardrailModelVerdict(guardrail string, cause error) GuardrailVerdict {
	return GuardrailVerdict{
		Guardrail: guardrail,
		Outcome:   "block",
		Severity:  "high",
		Category:  "infra",
		Summary:   fmt.Sprintf("guardrail %s could not run: no judge model is configured for it (%v). This is a configuration failure, not a finding about the work under review.", guardrail, cause),
		Issues: []string{
			"no judge model resolved at any level — set one of: PRD guardrail_model, task guardrail_model, autonomous.guardrail_model, autonomous.verification_model, autonomous.planning_model",
		},
		VerdictAt: time.Now(),
	}
}
