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

// guardrail resolution with a task-level override: the task's own
// backend wins, and its own model survives — the PRD-level
// claude-code pin is skipped entirely (a bare Claude alias is not
// servable by the pi-family backend, and the task-level model tier is
// reached before the global tiers anyway).
func TestGuardrailResolution_TaskOverrideDropsStaleModel(t *testing.T) {
	task := &Task{GuardrailBackend: "pi", GuardrailModel: "qwen3.8-64k:27b"}
	prd := &PRD{GuardrailBackend: "claude-code", GuardrailModel: "sonnet"}
	backend, model, err := resolveUnitGuardrailModelLocked(Config{}, task, prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend != "pi" || model != "qwen3.8-64k:27b" {
		t.Errorf("task override: got (%q, %q), want (pi, qwen3.8-64k:27b)", backend, model)
	}
}

// Same shape as the case above, over a different backend: the task's
// openwebui backend and model beat the PRD-level claude-code pin.
func TestGuardrailResolution_TaskOverrideDropsStaleModel_OpenWebUI(t *testing.T) {
	task := &Task{GuardrailBackend: "openwebui", GuardrailModel: "llama3"}
	prd := &PRD{GuardrailBackend: "claude-code", GuardrailModel: "sonnet"}
	backend, model, err := resolveUnitGuardrailModelLocked(Config{}, task, prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend != "openwebui" || model != "llama3" {
		t.Errorf("openwebui task override: got (%q, %q), want (openwebui, llama3)", backend, model)
	}
}

// Task inherits the PRD's backend and supplies its own model.
// v9.10.18 — the PRD-level model pin is consulted FIRST (tier 1) and,
// if set, wins; here it is unset, so the task's own model is the
// resolving tier.
func TestGuardrailResolution_TaskOwnModelWinsWhenBackendInherited(t *testing.T) {
	task := &Task{GuardrailModel: "haiku"}
	prd := &PRD{GuardrailBackend: "pi"}
	backend, model, err := resolveUnitGuardrailModelLocked(Config{}, task, prd)
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
	backend, model, err := resolveUnitGuardrailModelLocked(cfg, nil, prd)
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
	backend, model, err := resolveUnitGuardrailModelLocked(Config{}, nil, nil)
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
