package autonomous

import (
	"testing"
)

// TestClaudeExplicitModelWorker covers the explicit-model policy at the end
// of the worker-execution model resolution (resolveTaskExecutionLLM): a
// claude-code worker with an empty resolved model gets the daemon default
// (sonnet, or haiku on a mechanical task type), an operator-named model
// passes through unchanged, and non-claude backends are untouched.
func TestClaudeExplicitModelWorker(t *testing.T) {
	prd := &PRD{}

	t.Run("unpinned claude-code worker on a regular task resolves to sonnet", func(t *testing.T) {
		task := &Task{
			Backend: "claude-code",
			Title:   "implement the retry budget guard in the worker",
			Spec:    "track consecutive budget failures and stop the worker after the ceiling",
		}
		backend, model, source := resolveTaskExecutionLLM(task, nil, prd, nil, "pi")
		if backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", backend, "claude-code")
		}
		if source != ExecutorSourceTask {
			t.Errorf("source = %q, want %q", source, ExecutorSourceTask)
		}
		if model != DefaultClaudeModel {
			t.Errorf("model = %q, want %q", model, DefaultClaudeModel)
		}
	})

	t.Run("unpinned claude-code worker inherited from the story resolves to sonnet", func(t *testing.T) {
		task := &Task{Title: "implement the retry budget guard in the worker"}
		story := &Story{Backend: "claude-code"}
		backend, model, source := resolveTaskExecutionLLM(task, story, prd, nil, "pi")
		if backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", backend, "claude-code")
		}
		if source != ExecutorSourceStory {
			t.Errorf("source = %q, want %q", source, ExecutorSourceStory)
		}
		if model != DefaultClaudeModel {
			t.Errorf("model = %q, want %q", model, DefaultClaudeModel)
		}
	})

	t.Run("unpinned claude-code worker on a mechanical task resolves to haiku", func(t *testing.T) {
		task := &Task{
			Backend: "claude-code",
			Title:   "update the how-to docs for the retry budget",
			Spec:    "rewrite the retry budget section of the how-to page",
		}
		backend, model, _ := resolveTaskExecutionLLM(task, nil, prd, nil, "pi")
		if backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", backend, "claude-code")
		}
		if model != MechanicalClaudeModel {
			t.Errorf("model = %q, want %q (mechanical task type)", model, MechanicalClaudeModel)
		}
	})

	t.Run("task pin naming a premium claude-code model passes through unchanged", func(t *testing.T) {
		task := &Task{
			Backend: "claude-code",
			Model:   "claude-opus-4-1",
			Title:   "implement the retry budget guard in the worker",
		}
		backend, model, source := resolveTaskExecutionLLM(task, nil, prd, nil, "pi")
		if backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", backend, "claude-code")
		}
		if source != ExecutorSourceTask {
			t.Errorf("source = %q, want %q", source, ExecutorSourceTask)
		}
		if model != "claude-opus-4-1" {
			t.Errorf("model = %q, want %q (operator-named model must pass through)", model, "claude-opus-4-1")
		}
	})

	t.Run("pi-family backend with an empty model is left alone", func(t *testing.T) {
		task := &Task{Backend: "pi"}
		backend, model, _ := resolveTaskExecutionLLM(task, nil, prd, nil, "claude-code")
		if backend != "pi" {
			t.Fatalf("backend = %q, want %q", backend, "pi")
		}
		if model != "" {
			t.Errorf("model = %q, want empty (non-claude backends are untouched)", model)
		}
	})

	t.Run("burn guard reroute landing on claude-code gets the daemon default", func(t *testing.T) {
		SetBackendRoute(func(backend string) (string, bool) {
			if backend == "pi" {
				return "claude-code", true
			}
			return "", false
		})
		t.Cleanup(func() { SetBackendRoute(nil) })
		task := &Task{
			Backend: "pi",
			Model:   "anthropic/claude-sonnet-5-5",
			Title:   "implement the retry budget guard in the worker",
		}
		backend, model, source := resolveTaskExecutionLLM(task, nil, prd, nil, "pi")
		if backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", backend, "claude-code")
		}
		if source != ExecutorSourceBurnGuard {
			t.Errorf("source = %q, want %q", source, ExecutorSourceBurnGuard)
		}
		// The reroute drops the old backend's model (empty), so the policy
		// must supply the daemon default for the claude-code backend.
		if model != DefaultClaudeModel {
			t.Errorf("model = %q, want %q", model, DefaultClaudeModel)
		}
	})
}

// TestClaudeExplicitModelVerifier covers the explicit-model policy at the end
// of the verification model resolution (ResolveVerificationLLM): a
// claude-code verification backend with no model at any level resolves to
// sonnet, an operator-named model passes through unchanged, and non-claude
// backends are untouched.
func TestClaudeExplicitModelVerifier(t *testing.T) {
	mkTask := func() (*PRD, *Task) {
		task := &Task{ID: "task-1", Title: "implement the retry budget guard in the worker"}
		prd := &PRD{Story: []Story{{ID: "story-1", Tasks: []Task{*task}}}}
		return prd, task
	}

	t.Run("claude-code verification backend and no model at any level resolves to sonnet", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.VerificationBackend = "claude-code"
		prd, task := mkTask()
		out := ResolveVerificationLLM(prd, task, nil, cfg)
		if out.Backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", out.Backend, "claude-code")
		}
		if out.Source != VerifierSourceGlobal {
			t.Errorf("source = %q, want %q", out.Source, VerifierSourceGlobal)
		}
		if out.Model != DefaultClaudeModel {
			t.Errorf("model = %q, want %q", out.Model, DefaultClaudeModel)
		}
	})

	t.Run("claude-code task pin with no model resolves to sonnet", func(t *testing.T) {
		cfg := DefaultConfig()
		prd, task := mkTask()
		task.VerificationBackend = "claude-code"
		out := ResolveVerificationLLM(prd, task, nil, cfg)
		if out.Backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", out.Backend, "claude-code")
		}
		if out.Source != VerifierSourceTask {
			t.Errorf("source = %q, want %q", out.Source, VerifierSourceTask)
		}
		if out.Model != DefaultClaudeModel {
			t.Errorf("model = %q, want %q", out.Model, DefaultClaudeModel)
		}
	})

	t.Run("task pin naming a premium claude-code model passes through unchanged", func(t *testing.T) {
		cfg := DefaultConfig()
		prd, task := mkTask()
		task.VerificationBackend = "claude-code"
		task.VerificationModel = "claude-opus-4-1"
		out := ResolveVerificationLLM(prd, task, nil, cfg)
		if out.Backend != "claude-code" {
			t.Fatalf("backend = %q, want %q", out.Backend, "claude-code")
		}
		if out.Model != "claude-opus-4-1" {
			t.Errorf("model = %q, want %q (operator-named model must pass through)", out.Model, "claude-opus-4-1")
		}
	})

	t.Run("non-claude verification backend with an empty model is left alone", func(t *testing.T) {
		cfg := DefaultConfig()
		prd, task := mkTask()
		task.VerificationBackend = "pi"
		out := ResolveVerificationLLM(prd, task, nil, cfg)
		if out.Backend != "pi" {
			t.Fatalf("backend = %q, want %q", out.Backend, "pi")
		}
		if out.Model != "" {
			t.Errorf("model = %q, want empty (non-claude backends are untouched)", out.Model)
		}
	})
}
