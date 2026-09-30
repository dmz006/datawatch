// v8.37.2 — resolveTaskBackendModel regression test.
//
// Found live: a task stalled repeatedly on opencode/ollama/qwen3.8:27b.
// Overriding just the task's backend to claude-code (via set_task_llm,
// leaving model empty — "empty = backend default" per that tool's own
// contract) still passed the PRD's stale "ollama/qwen3.8:27b" model
// string through, because backend and model used to cascade through
// task → story → PRD independently. claude-code doesn't understand an
// Ollama model name and got stuck asking an interactive "unknown model"
// question instead of completing the one-shot task.

package autonomous

import "testing"

func TestResolveTaskBackendModel_TaskBackendOverrideDropsStaleModel(t *testing.T) {
	prd := &PRD{Backend: "opencode", Model: "ollama/qwen3.8:27b"}
	task := &Task{Backend: "claude-code", Model: ""}

	backend, model := resolveTaskBackendModel(task, nil, prd, "opencode")

	if backend != "claude-code" {
		t.Errorf("backend = %q, want claude-code", backend)
	}
	if model != "" {
		t.Errorf("model = %q, want empty (backend default) — stale PRD model %q leaked through", model, prd.Model)
	}
}

func TestResolveTaskBackendModel_TaskOwnModelWinsRegardlessOfBackendLevel(t *testing.T) {
	prd := &PRD{Backend: "opencode", Model: "ollama/qwen3.8:27b"}
	task := &Task{Backend: "", Model: "claude-opus-5"}

	backend, model := resolveTaskBackendModel(task, nil, prd, "opencode")

	if backend != "opencode" {
		t.Errorf("backend = %q, want opencode (inherited)", backend)
	}
	if model != "claude-opus-5" {
		t.Errorf("model = %q, want claude-opus-5 (task's own override preserved)", model)
	}
}

func TestResolveTaskBackendModel_StoryOverridePairsItsOwnModel(t *testing.T) {
	prd := &PRD{Backend: "opencode", Model: "ollama/qwen3.8:27b"}
	story := &Story{Backend: "claude-code", Model: "claude-sonnet-5"}
	task := &Task{}

	backend, model := resolveTaskBackendModel(task, story, prd, "opencode")

	if backend != "claude-code" || model != "claude-sonnet-5" {
		t.Errorf("got backend=%q model=%q, want claude-code/claude-sonnet-5", backend, model)
	}
}

func TestResolveTaskBackendModel_NothingSetFallsThroughToPRD(t *testing.T) {
	prd := &PRD{Backend: "opencode", Model: "ollama/qwen3.8:27b"}
	task := &Task{}

	backend, model := resolveTaskBackendModel(task, nil, prd, "claude-code")

	if backend != "opencode" || model != "ollama/qwen3.8:27b" {
		t.Errorf("got backend=%q model=%q, want opencode/ollama/qwen3.8:27b", backend, model)
	}
}
