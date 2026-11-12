package autonomous

import "testing"

func TestResolveTaskExecutionLLMHonoursBackendRoute(t *testing.T) {
	prd := &PRD{Backend: "claude-code", Model: "sonnet"}
	t.Cleanup(func() { SetBackendRoute(nil) })

	b, m, src := resolveTaskExecutionLLM(&Task{}, nil, prd, nil, "")
	if b != "claude-code" || m != "sonnet" || src != ExecutorSourcePRD {
		t.Fatalf("unrouted = %q %q %q", b, m, src)
	}
	SetBackendRoute(func(from string) (string, bool) {
		if from == "claude-code" {
			return "pi-prompt", true
		}
		return "", false
	})
	b, m, src = resolveTaskExecutionLLM(&Task{}, nil, prd, nil, "")
	if b != "pi-prompt" || m != "" || src != ExecutorSourceBurnGuard {
		t.Fatalf("routed = %q %q %q", b, m, src)
	}
	// A backend the guard does not route is untouched.
	b, _, _ = resolveTaskExecutionLLM(&Task{Backend: "opencode"}, nil, prd, nil, "")
	if b != "opencode" {
		t.Fatalf("other backend = %q", b)
	}
	SetBackendRoute(nil)
	if b, _, _ = resolveTaskExecutionLLM(&Task{}, nil, prd, nil, ""); b != "claude-code" {
		t.Fatalf("cleared route = %q", b)
	}
}
