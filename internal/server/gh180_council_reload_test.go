// GH#180 (second bug in the report) — PUT /api/config {"council.llm_ref":
// ...} + POST /api/reload persisted the new value to config.yaml and
// s.cfg.Council.LLMRef, but the live council.Orchestrator kept
// resolving whatever LLM was configured at daemon startup (LLMRef was
// a plain struct field set once in runStart, cmd/datawatch/main.go,
// and never revisited).

package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/council"
)

type fakeCouncilOrchestratorForReload struct {
	fakeCouncilOrchestratorForImage // reuse the existing no-op base
	gotLLMRef                       string
	gotBackends                     []string
	gotMaxParallel                  int
	setCalls                        int
}

func (f *fakeCouncilOrchestratorForReload) SetLLMConfig(llmRef string, backends []string, maxParallel int) {
	f.gotLLMRef = llmRef
	f.gotBackends = backends
	f.gotMaxParallel = maxParallel
	f.setCalls++
}

func TestReload_PropagatesCouncilLLMRefToOrchestrator(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Council.LLMRef = "ollama"
	fake := &fakeCouncilOrchestratorForReload{}
	s.councilOrch = fake

	newCfg := *s.cfg
	newCfg.Council.LLMRef = "claude-code-prod"
	newCfg.Council.MaxParallel = 4
	newCfg.Council.Backends = []string{"claude-code-prod", "ollama"}
	if err := config.Save(&newCfg, s.cfgPath); err != nil {
		t.Fatalf("save: %v", err)
	}

	res := s.reload()
	if !res.OK {
		t.Fatalf("reload failed: %+v", res)
	}

	if fake.setCalls != 1 {
		t.Fatalf("SetLLMConfig call count = %d, want 1", fake.setCalls)
	}
	if fake.gotLLMRef != "claude-code-prod" {
		t.Errorf("orchestrator LLMRef = %q, want %q — still resolving the startup value", fake.gotLLMRef, "claude-code-prod")
	}
	if fake.gotMaxParallel != 4 {
		t.Errorf("orchestrator MaxParallel = %d, want 4", fake.gotMaxParallel)
	}
	if len(fake.gotBackends) != 2 {
		t.Errorf("orchestrator Backends = %v, want 2 entries", fake.gotBackends)
	}
	if s.cfg.Council.LLMRef != "claude-code-prod" {
		t.Errorf("s.cfg.Council.LLMRef = %q, want %q", s.cfg.Council.LLMRef, "claude-code-prod")
	}
	if !contains(res.Applied, "council.llm_ref") {
		t.Errorf("expected council.llm_ref in Applied, got %+v", res.Applied)
	}
}

func TestReload_NoCouncilChange_DoesNotCallSetLLMConfig(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Council.LLMRef = "ollama"
	fake := &fakeCouncilOrchestratorForReload{}
	s.councilOrch = fake

	newCfg := *s.cfg // identical council config
	if err := config.Save(&newCfg, s.cfgPath); err != nil {
		t.Fatalf("save: %v", err)
	}

	res := s.reload()
	if !res.OK {
		t.Fatalf("reload failed: %+v", res)
	}
	if fake.setCalls != 0 {
		t.Errorf("SetLLMConfig should not fire when council config is unchanged, got %d calls", fake.setCalls)
	}
}

// Sanity check that council.Orchestrator itself satisfies the
// interface's SetLLMConfig contract this test's fake stands in for.
func TestOrchestrator_SetLLMConfig(t *testing.T) {
	o := council.NewOrchestrator(t.TempDir())
	o.LLMRef = "ollama"
	o.MaxParallel = 2
	o.SetLLMConfig("claude-code-prod", []string{"a", "b"}, 5)
	if o.LLMRef != "claude-code-prod" || o.MaxParallel != 5 || len(o.Backends) != 2 {
		t.Fatalf("SetLLMConfig did not update fields: LLMRef=%q MaxParallel=%d Backends=%v", o.LLMRef, o.MaxParallel, o.Backends)
	}
}
