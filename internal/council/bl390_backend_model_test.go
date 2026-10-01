// BL390 (v8.38.0) — Phase 1: per-persona backend/model resolution and
// capacity admission.

package council

import (
	"context"
	"sync"
	"testing"
)

func TestResolvePersonaBackendModel_PersonaBackendWinsWithItsOwnModel(t *testing.T) {
	o := &Orchestrator{LLMRef: "ollama"}
	p := Persona{Name: "x", Backend: "claude-code", Model: "claude-sonnet-5"}

	backend, model := o.resolvePersonaBackendModel(p)

	if backend != "claude-code" || model != "claude-sonnet-5" {
		t.Errorf("got backend=%q model=%q, want claude-code/claude-sonnet-5", backend, model)
	}
}

func TestResolvePersonaBackendModel_NoOverrideFallsBackToCouncilDefault(t *testing.T) {
	o := &Orchestrator{LLMRef: "ollama"}
	p := Persona{Name: "x"}

	backend, model := o.resolvePersonaBackendModel(p)

	if backend != "ollama" || model != "" {
		t.Errorf("got backend=%q model=%q, want ollama/\"\" (registry default)", backend, model)
	}
}

func TestResolvePersonaBackendModel_ModelAloneWithoutBackendIsIgnored(t *testing.T) {
	// Mirrors the v8.37.2 autonomous fix's reasoning: a Model set on
	// the persona without a Backend override has nowhere safe to
	// attach (it would pair with the council's default backend, which
	// may not even be the same kind the model name was written for).
	// resolvePersonaBackendModel only reads Model alongside Backend.
	o := &Orchestrator{LLMRef: "ollama"}
	p := Persona{Name: "x", Model: "claude-sonnet-5"}

	backend, model := o.resolvePersonaBackendModel(p)

	if backend != "ollama" || model != "" {
		t.Errorf("got backend=%q model=%q, want ollama/\"\" — a bare Model with no Backend must not leak through", backend, model)
	}
}

func TestOrchestrator_CapacityAdmitFn_CalledOncePerPersonaAndReleased(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	o.LLMRef = "mock"
	o.InferenceFn = func(ctx context.Context, ref, model, sys, prompt, consumer string) (string, string, error) {
		return "reply", "", nil
	}

	var mu sync.Mutex
	admits := map[string]int{}
	released := map[string]int{}
	o.CapacityAdmitFn = func(ctx context.Context, backend, model, holder, prdID string) (func(), error) {
		mu.Lock()
		admits[holder]++
		mu.Unlock()
		return func() {
			mu.Lock()
			released[holder]++
			mu.Unlock()
		}, nil
	}

	run, err := o.Run("proposal", []string{"security-skeptic", "ux-advocate"}, ModeQuick)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// 2 personas + 1 synthesis call = 3 distinct holders, each admitted
	// and released exactly once.
	wantHolders := []string{
		run.ID + "-security-skeptic",
		run.ID + "-ux-advocate",
		run.ID + "-synthesis",
	}
	for _, h := range wantHolders {
		if admits[h] != 1 {
			t.Errorf("holder %q: admitted %d times, want 1", h, admits[h])
		}
		if released[h] != 1 {
			t.Errorf("holder %q: released %d times, want 1", h, released[h])
		}
	}
}

func TestOrchestrator_CapacityAdmitFn_DenialSurfacesAsPersonaError(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	o.LLMRef = "mock"
	o.InferenceFn = func(ctx context.Context, ref, model, sys, prompt, consumer string) (string, string, error) {
		t.Error("InferenceFn should not be called when capacity admission is denied")
		return "", "", nil
	}
	o.CapacityAdmitFn = func(ctx context.Context, backend, model, holder, prdID string) (func(), error) {
		return func() {}, context.DeadlineExceeded
	}

	run, err := o.Run("proposal", []string{"security-skeptic"}, ModeQuick)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	resp := run.Rounds[0].Responses["security-skeptic"]
	if resp == "" || resp[:len("[security-skeptic] error:")] != "[security-skeptic] error:" {
		t.Errorf("response = %q, want a capacity-error-prefixed string", resp)
	}
}
