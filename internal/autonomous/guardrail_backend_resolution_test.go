// BL416 Phase 1 — resolveGuardrailBackendModel tests.
//
// Guardrail backend/model routing mirrors resolveTaskBackendModel's
// v8.37.2 pairing rule over task → PRD → global (the existing
// verification_backend/verification_model config): model is taken from
// the SAME level backend was found at, so a stale model string from a
// less-specific level never leaks onto a backend it wasn't configured
// for. The guardrail fields are independent of the execution
// Backend/Model fields.
//
// Fixed live 2026-10-12 (BL418 live validation of the first real,
// non-ask-compatible guardrail call this config ever made): when
// nothing is set at the task/PRD guardrail level, BACKEND still falls
// through to the global default, but MODEL does not — see
// resolveGuardrailBackendModel's own doc comment and the "NothingSet"
// test below for why.

package autonomous

import "testing"

func TestResolveGuardrailBackendModel_TaskOverrideDropsStaleModel(t *testing.T) {
	prd := &PRD{GuardrailBackend: "ollama", GuardrailModel: "qwen3.8:27b"}
	task := &Task{GuardrailBackend: "claude-code", GuardrailModel: ""}

	backend, model := resolveGuardrailBackendModel(task, prd, "ollama", "llama3")

	if backend != "claude-code" {
		t.Errorf("backend = %q, want claude-code", backend)
	}
	if model != "" {
		t.Errorf("model = %q, want empty (backend default) — stale PRD/global model leaked through", model)
	}
}

func TestResolveGuardrailBackendModel_TaskOwnModelWinsWhenBackendInherited(t *testing.T) {
	prd := &PRD{GuardrailBackend: "ollama", GuardrailModel: "qwen3.8:27b"}
	task := &Task{GuardrailModel: "gemma4"}

	backend, model := resolveGuardrailBackendModel(task, prd, "openwebui", "llama3")

	if backend != "ollama" {
		t.Errorf("backend = %q, want ollama (inherited from PRD)", backend)
	}
	if model != "gemma4" {
		t.Errorf("model = %q, want gemma4 (task's own override preserved)", model)
	}
}

func TestResolveGuardrailBackendModel_PRDOverridePairsItsOwnModelAndBeatsGlobal(t *testing.T) {
	prd := &PRD{GuardrailBackend: "openwebui", GuardrailModel: "mistral"}
	task := &Task{}

	backend, model := resolveGuardrailBackendModel(task, prd, "ollama", "llama3")

	if backend != "openwebui" || model != "mistral" {
		t.Errorf("got backend=%q model=%q, want openwebui/mistral", backend, model)
	}
}

func TestResolveGuardrailBackendModel_NothingSet_BackendFallsThroughButNotModel(t *testing.T) {
	// Found live 2026-10-12 (BL418 live validation): the old version of
	// this test asserted model=="llama3" here, which is exactly the bug
	// — a flat ask-only global model baked into the return value reached
	// a B124 session-routed guardrail call, which treats any non-empty
	// GuardrailModel as a genuine per-unit override and sends it
	// straight to a real tool-using session. A real claude-code session
	// rejected "qwen3.8:27b" outright and hung for its full timeout.
	// Backend has no equivalent ambiguity (same value either way), so
	// it still falls through; model must not, so each caller's own
	// fallback (ask-path global, or session-path registry-default) is
	// free to apply the right one for its own routing decision.
	prd := &PRD{}
	task := &Task{}

	backend, model := resolveGuardrailBackendModel(task, prd, "ollama", "llama3")

	if backend != "ollama" {
		t.Errorf("backend = %q, want ollama (global fallback)", backend)
	}
	if model != "" {
		t.Errorf("model = %q, want empty — global model must not leak into a session-routed guardrail call", model)
	}
}

func TestResolveGuardrailBackendModel_PRDBackendWithEmptyModelDoesNotInheritGlobalModel(t *testing.T) {
	prd := &PRD{GuardrailBackend: "openwebui"}
	task := &Task{}

	backend, model := resolveGuardrailBackendModel(task, prd, "ollama", "llama3")

	if backend != "openwebui" {
		t.Errorf("backend = %q, want openwebui", backend)
	}
	if model != "" {
		t.Errorf("model = %q, want empty (backend default) — global model leaked past PRD backend override", model)
	}
}

func TestResolveGuardrailBackendModel_IndependentOfExecutionBackendModel(t *testing.T) {
	prd := &PRD{Backend: "opencode", Model: "ollama/qwen3.8:27b"}
	task := &Task{Backend: "claude-code", Model: "claude-opus-5"}

	backend, model := resolveGuardrailBackendModel(task, prd, "ollama", "llama3")

	if backend != "ollama" {
		t.Errorf("backend = %q, want ollama (global fallback; execution Backend/Model must not affect guardrail routing)", backend)
	}
	if model != "" {
		t.Errorf("model = %q, want empty (no guardrail-level override set anywhere; execution Backend/Model must not affect guardrail routing)", model)
	}
}
