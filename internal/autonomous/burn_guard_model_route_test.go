package autonomous

import "testing"

// TestResolveTaskExecutionLLMHonoursBurnGuardModelRoute: the route_cheaper_model rung
// keeps the backend and replaces the model for new work; a backend route takes
// precedence (once the backend moves, the model is dropped); clearing the model route
// restores the original. A backend the guard does not model-route is untouched.
func TestResolveTaskExecutionLLMHonoursBurnGuardModelRoute(t *testing.T) {
	prd := &PRD{Backend: "claude-code", Model: "sonnet"}
	t.Cleanup(func() {
		SetBackendModelRoute(nil)
		SetBackendRoute(nil)
	})

	// Baseline: unrouted.
	b, m, src := resolveTaskExecutionLLM(&Task{}, nil, prd, nil, "")
	if b != "claude-code" || m != "sonnet" || src != ExecutorSourcePRD {
		t.Fatalf("unrouted = %q %q %q", b, m, src)
	}

	// route_cheaper_model: the backend is kept, the model becomes the cheaper one.
	SetBackendModelRoute(func(backend string) (string, bool) {
		if backend == "claude-code" {
			return "haiku", true
		}
		return "", false
	})
	b, m, src = resolveTaskExecutionLLM(&Task{}, nil, prd, nil, "")
	if b != "claude-code" || m != "haiku" || src != ExecutorSourceBurnGuard {
		t.Fatalf("model-routed = %q %q %q (want claude-code haiku burn_guard)", b, m, src)
	}

	// A backend the guard does not model-route is untouched.
	if b, m, _ := resolveTaskExecutionLLM(&Task{Backend: "opencode"}, nil, prd, nil, ""); b != "opencode" || m != "" {
		t.Fatalf("other backend = %q %q (want opencode '')", b, m)
	}

	// A backend route takes precedence: once the backend moves, the model is dropped
	// and the original model is not applied to the target backend.
	SetBackendRoute(func(from string) (string, bool) {
		if from == "claude-code" {
			return "pi-prompt", true
		}
		return "", false
	})
	b, m, src = resolveTaskExecutionLLM(&Task{}, nil, prd, nil, "")
	if b != "pi-prompt" || m != "" || src != ExecutorSourceBurnGuard {
		t.Fatalf("backend route wins = %q %q %q (want pi-prompt '' burn_guard)", b, m, src)
	}
	SetBackendRoute(nil)

	// Clearing the model route restores the original model.
	SetBackendModelRoute(nil)
	if b, m, src := resolveTaskExecutionLLM(&Task{}, nil, prd, nil, ""); b != "claude-code" || m != "sonnet" || src != ExecutorSourcePRD {
		t.Fatalf("cleared model route = %q %q %q (want claude-code sonnet prd)", b, m, src)
	}
}
