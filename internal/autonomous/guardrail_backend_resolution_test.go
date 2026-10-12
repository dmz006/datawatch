// BL416 Phase 1 — resolveGuardrailBackendModel tests.
//
// Guardrail backend/model routing mirrors resolveTaskBackendModel's
// v8.37.2 pairing rule over task → PRD → global (the existing
// verification_backend/verification_model config): model is taken from
// the SAME level backend was found at, so a stale model string from a
// less-specific level never leaks onto a backend it wasn't configured
// for. The guardrail fields are independent of the execution
// Backend/Model fields.

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

func TestResolveGuardrailBackendModel_NothingSetFallsThroughToGlobal(t *testing.T) {
	prd := &PRD{}
	task := &Task{}

	backend, model := resolveGuardrailBackendModel(task, prd, "ollama", "llama3")

	if backend != "ollama" || model != "llama3" {
		t.Errorf("got backend=%q model=%q, want ollama/llama3", backend, model)
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

	if backend != "ollama" || model != "llama3" {
		t.Errorf("got backend=%q model=%q, want ollama/llama3 (execution Backend/Model must not affect guardrail routing)", backend, model)
	}
}
