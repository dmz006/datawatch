// BL416 Phase 1 — guardrail backend/model resolution tests.
//
// v9.10.18 note: these originally exercised
// resolveGuardrailBackendModel, which was removed because its model
// half returned an empty model whenever nothing was pinned (the
// caller then had to fall back, and a pi-family judge silently ran on
// the backend's built-in default model). The backend precedence
// contract they encode (task → PRD → global verification_backend) is
// preserved verbatim by resolveUnitGuardrailModelLocked, now paired
// with the guaranteed model resolution of ResolveGuardrailModel — so
// the "NothingSet" case below now expects ErrNoGuardrailModel instead
// of an empty model.
package autonomous

import (
	"errors"
	"testing"
)

// v9.10.18 — the PRD's own guardrail_model pin is tier 1: it still
// resolves on the task's backend (which wins backend resolution) when
// it is servable by it. A bare Claude alias pinned at the PRD level is
// never paired with a pi-family backend (see the unpairably-pinned
// case below, where the task's own model resolves instead).
func TestGuardrailResolution_TaskBackendWinsPRDModelPinResolves(t *testing.T) {
	task := &Task{GuardrailBackend: "pi", GuardrailModel: "qwen3.8-64k:27b"}
	prd := &PRD{GuardrailBackend: "claude-code", GuardrailModel: "sonnet"}
	backend, model, _, err := resolveUnitGuardrailModelLocked(Config{}, task, prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend != "pi" || model != "sonnet" {
		t.Errorf("got (%q, %q), want (pi, sonnet) — task backend wins, PRD pin is tier 1", backend, model)
	}
}

// The pairing check protects tier 1: a bare Claude alias pinned at the
// PRD level cannot be served by the pi-family task backend, so that
// tier is skipped and the task's own pin (tier 2) resolves instead —
// the original "drops stale model" case, now under the v9.10.18 tier
// order.
func TestGuardrailResolution_TaskBackendDropsUnpairablePRDPin(t *testing.T) {
	task := &Task{GuardrailBackend: "pi", GuardrailModel: "qwen3.8-64k:27b"}
	prd := &PRD{GuardrailBackend: "claude-code", GuardrailModel: "claude-opus-5-5"}
	backend, model, _, err := resolveUnitGuardrailModelLocked(Config{}, task, prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend != "pi" || model != "qwen3.8-64k:27b" {
		t.Errorf("got (%q, %q), want (pi, qwen3.8-64k:27b) — unpairable PRD pin skipped", backend, model)
	}
}

// Task inherits the PRD's backend and supplies its own model.
// v9.10.18 — the PRD-level model pin is consulted FIRST (tier 1) and,
// if set, wins; here it is unset, so the task's own model is the
// resolving tier.
func TestGuardrailResolution_TaskOwnModelWinsWhenBackendInherited(t *testing.T) {
	task := &Task{GuardrailModel: "haiku"}
	prd := &PRD{GuardrailBackend: "pi"}
	backend, model, _, err := resolveUnitGuardrailModelLocked(Config{}, task, prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend != "pi" || model != "haiku" {
		t.Errorf("inherited-backend case: got (%q, %q), want (pi, haiku)", backend, model)
	}
}

// PRD-level guardrail backend with its own model: the PRD's pin is
// tier 1 and resolves, beating the global tiers (the old name's
// "beats global" semantics, preserved).
func TestGuardrailResolution_PRDOverridePairsItsOwnModelAndBeatsGlobal(t *testing.T) {
	prd := &PRD{GuardrailBackend: "claude-code", GuardrailModel: "sonnet"}
	cfg := Config{
		VerificationBackend: "pi",
		VerificationModel:   "qwen3.8-64k:27b",
		GuardrailModel:      "global-guardrail",
		PlanningModel:       "global-planning",
	}
	backend, model, _, err := resolveUnitGuardrailModelLocked(cfg, nil, prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend != "claude-code" || model != "sonnet" {
		t.Errorf("PRD pin case: got (%q, %q), want (claude-code, sonnet)", backend, model)
	}
}

// v9.10.18 — nothing set anywhere: the old contract returned an empty
// model (backend default), which is exactly the bug being removed. The
// new contract refuses to resolve and reports a typed error so the
// guardrail records a loud, non-retried infra block instead of
// launching a judge on the backend's built-in default.
func TestGuardrailResolution_NothingSet_FailsLoud(t *testing.T) {
	backend, model, _, err := resolveUnitGuardrailModelLocked(Config{}, nil, nil)
	if err == nil {
		t.Fatalf("expected ErrNoGuardrailModel, got backend=%q model=%q with nil error", backend, model)
	}
	if model != "" {
		t.Errorf("model must be empty on failure, got %q", model)
	}
	if !errors.Is(err, ErrNoGuardrailModel) {
		t.Errorf("expected ErrNoGuardrailModel, got: %v", err)
	}
}

// Only a task or PRD backend override pins the judge. The production
// fixture (global verification backend pi-prompt, no override) resolves to
// that backend but is not pinned, so the judge policy order and the size
// rule still apply to it.
func TestGuardrailResolution_PinnedOnlyForTaskOrPRDOverride(t *testing.T) {
	cfg := Config{
		VerificationBackend: "pi-prompt",
		VerificationModel:   "qwen3.8-64k:27b",
		JudgePolicy:         []string{"pi-prompt", "claude-code"},
	}
	cases := []struct {
		name       string
		task       *Task
		prd        *PRD
		wantBack   string
		wantPinned bool
	}{
		{"no override", nil, nil, "pi-prompt", false},
		{"empty task and PRD", &Task{}, &PRD{}, "pi-prompt", false},
		{"task override", &Task{GuardrailBackend: "pi-prompt"}, &PRD{}, "pi-prompt", true},
		{"PRD override", &Task{}, &PRD{GuardrailBackend: "claude-code", GuardrailModel: "sonnet"}, "claude-code", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			backend, _, pinned, err := resolveUnitGuardrailModelLocked(cfg, c.task, c.prd)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if backend != c.wantBack || pinned != c.wantPinned {
				t.Errorf("got (%q, pinned=%v), want (%q, pinned=%v)", backend, pinned, c.wantBack, c.wantPinned)
			}
		})
	}
}
