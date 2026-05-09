// v7.0.0 S3 — replaces the v6.22.2 BL289 stub-fallback contract.
//
// Operator-decided 2026-05-08 (BL295 ASK Q7): the v6.x stub strings
// ("[name] STUB — proposal length...", "(stub mode)") were misleading
// — operators thought Council Mode worked when it was just a placeholder.
// v7.0.0 strips the stubs entirely; Run() now returns a clear
// ErrNoInference when no LLM is wired so operators know to wire one.
//
// BL289 (no feature may REQUIRE Ollama to function) is preserved at
// the daemon level — the daemon starts cleanly without an LLM, every
// other feature works, only Council declines to fake it. The cfg
// shim auto-migrates cfg.ollama.host into the registry so existing
// operators get Council working with zero config changes.

package council

import (
	"context"
	"errors"
	"testing"
)

func TestRunCtx_NoInferenceFn_ReturnsErrNoInference(t *testing.T) {
	o := &Orchestrator{
		personas:    map[string]Persona{"p": {Name: "p", SystemPrompt: "x"}},
		MaxParallel: 1,
		cancels:     map[string]context.CancelFunc{},
		// InferenceFn intentionally nil — the v6.x stub path.
	}
	_, err := o.RunCtx(context.Background(), "should we cache?", nil, ModeQuick)
	if err == nil {
		t.Fatal("expected ErrNoInference when no inference wired")
	}
	if !errors.Is(err, ErrNoInference) {
		t.Fatalf("want ErrNoInference, got %v", err)
	}
}

func TestRunCtx_EmptyLLMRef_ReturnsErrNoInference(t *testing.T) {
	o := &Orchestrator{
		personas:    map[string]Persona{"p": {Name: "p", SystemPrompt: "x"}},
		MaxParallel: 1,
		cancels:     map[string]context.CancelFunc{},
		LLMRef:      "", // empty — bug or operator forgot to set it
		InferenceFn: func(ctx context.Context, ref, sys, prompt, consumer string) (string, string, error) {
			return "ok", "gpu-1", nil
		},
	}
	_, err := o.RunCtx(context.Background(), "x", nil, ModeQuick)
	if !errors.Is(err, ErrNoInference) {
		t.Fatalf("want ErrNoInference for empty LLMRef, got %v", err)
	}
}

func TestRunCtx_HappyPath_RealInference(t *testing.T) {
	o := &Orchestrator{
		personas: map[string]Persona{
			"p1": {Name: "p1", Role: "test", SystemPrompt: "first"},
			"p2": {Name: "p2", Role: "test", SystemPrompt: "second"},
		},
		MaxParallel: 2,
		cancels:     map[string]context.CancelFunc{},
		LLMRef:      "test-llm",
		InferenceFn: func(ctx context.Context, ref, sys, prompt, consumer string) (string, string, error) {
			return "response from " + sys, "gpu-1", nil
		},
	}
	run, err := o.RunCtx(context.Background(), "test", []string{"p1", "p2"}, ModeQuick)
	if err != nil {
		t.Fatalf("RunCtx: %v", err)
	}
	if len(run.Rounds) != 1 {
		t.Fatalf("rounds: got %d", len(run.Rounds))
	}
	r := run.Rounds[0]
	if len(r.Responses) != 2 {
		t.Fatalf("responses: got %d", len(r.Responses))
	}
	if r.Responses["p1"] == "" || r.Responses["p2"] == "" {
		t.Fatalf("missing response: %+v", r.Responses)
	}
}

func TestRunCtx_DebateMode_RunsThreeRounds(t *testing.T) {
	calls := 0
	o := &Orchestrator{
		personas: map[string]Persona{
			"p1": {Name: "p1", SystemPrompt: "s1"},
		},
		MaxParallel: 1,
		cancels:     map[string]context.CancelFunc{},
		LLMRef:      "test",
		InferenceFn: func(ctx context.Context, ref, sys, prompt, consumer string) (string, string, error) {
			calls++
			return "ok", "", nil
		},
	}
	_, err := o.RunCtx(context.Background(), "x", nil, ModeDebate)
	if err != nil {
		t.Fatalf("RunCtx: %v", err)
	}
	// 3 rounds × 1 persona = 3 persona calls + 1 synthesis call = 4.
	if calls != 4 {
		t.Fatalf("expected 4 inference calls (3 persona + 1 synthesis), got %d", calls)
	}
}

func TestRunCtx_PerPersonaErrorContinuesRun(t *testing.T) {
	o := &Orchestrator{
		personas: map[string]Persona{
			"p1": {Name: "p1", SystemPrompt: "s1"},
			"p2": {Name: "p2", SystemPrompt: "s2"},
		},
		MaxParallel: 2,
		cancels:     map[string]context.CancelFunc{},
		LLMRef:      "test",
		InferenceFn: func(ctx context.Context, ref, sys, prompt, consumer string) (string, string, error) {
			if sys == "s1" {
				return "", "", errors.New("simulated p1 failure")
			}
			return "ok from p2", "", nil
		},
	}
	run, err := o.RunCtx(context.Background(), "x", []string{"p1", "p2"}, ModeQuick)
	if err != nil {
		t.Fatalf("RunCtx: %v", err)
	}
	if len(run.Rounds[0].Responses) != 2 {
		t.Fatalf("expected both personas to surface (one error, one ok); got %d", len(run.Rounds[0].Responses))
	}
	p1 := run.Rounds[0].Responses["p1"]
	if p1 == "" || !contains2(p1, "error") {
		t.Fatalf("p1 should surface error wrapper, got %q", p1)
	}
}

func TestCancel(t *testing.T) {
	called := false
	o := &Orchestrator{
		personas:    map[string]Persona{"p": {Name: "p", SystemPrompt: "x"}},
		MaxParallel: 1,
		cancels:     map[string]context.CancelFunc{},
		LLMRef:      "test",
		InferenceFn: func(ctx context.Context, ref, sys, prompt, consumer string) (string, string, error) {
			called = true
			<-ctx.Done()
			return "", "", ctx.Err()
		},
	}
	go func() {
		// Wait until the call is in flight before cancelling.
		for !called {
		}
		// Find the running cancel func and trigger it.
		o.mu.Lock()
		var fn context.CancelFunc
		for _, f := range o.cancels {
			fn = f
			break
		}
		o.mu.Unlock()
		if fn != nil {
			fn()
		}
	}()
	run, _ := o.RunCtx(context.Background(), "x", nil, ModeQuick)
	if run == nil {
		t.Fatal("expected partial run record even on cancel")
	}
}

func contains2(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
