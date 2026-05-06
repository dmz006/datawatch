package council

import (
	"strings"
	"testing"
)

func TestDefaultPersonasTenCanonicalRoles(t *testing.T) {
	// v6.12.1 (BL276) — operator added platform-engineer, network-engineer,
	// data-architect, privacy. Total = 10.
	got := DefaultPersonas()
	if len(got) != 10 {
		t.Fatalf("default personas: %d (want 10)", len(got))
	}
	want := map[string]bool{
		"security-skeptic": true, "ux-advocate": true, "perf-hawk": true,
		"simplicity-advocate": true, "ops-realist": true, "contrarian": true,
		"platform-engineer": true, "network-engineer": true,
		"data-architect": true, "privacy": true,
	}
	for _, p := range got {
		if !want[p.Name] {
			t.Errorf("unexpected persona: %s", p.Name)
		}
		if p.SystemPrompt == "" {
			t.Errorf("persona %s missing system prompt", p.Name)
		}
	}
}

func TestNewOrchestratorSeedsFromEmptyDir(t *testing.T) {
	dir := t.TempDir()
	o := NewOrchestrator(dir)
	if got := len(o.Personas()); got != 10 {
		t.Errorf("seeded personas: %d (want 10)", got)
	}
}

func TestRunQuickModeOneRound(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	run, err := o.Run("Should we ship feature X?", []string{"security-skeptic", "ux-advocate"}, ModeQuick)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(run.Rounds) != 1 {
		t.Errorf("rounds: %d (want 1)", len(run.Rounds))
	}
	if got := len(run.Rounds[0].Responses); got != 2 {
		t.Errorf("responses: %d", got)
	}
	if run.Mode != ModeQuick {
		t.Errorf("mode: %s", run.Mode)
	}
}

func TestRunDebateModeThreeRounds(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	run, err := o.Run("proposal", []string{"contrarian"}, ModeDebate)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(run.Rounds) != 3 {
		t.Errorf("rounds: %d (want 3)", len(run.Rounds))
	}
}

func TestRunDefaultsAllPersonas(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	run, err := o.Run("p", nil, ModeQuick)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := len(run.Personas); got != 10 {
		t.Errorf("default personas in run: %d (want 10 — v6.12.1 added 4)", got)
	}
}

func TestRunUnknownPersonaErrors(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	if _, err := o.Run("p", []string{"nope"}, ModeQuick); err == nil {
		t.Error("expected unknown-persona error")
	}
}

func TestRunPersistedAndLoadable(t *testing.T) {
	dir := t.TempDir()
	o := NewOrchestrator(dir)
	run, err := o.Run("p", []string{"contrarian"}, ModeQuick)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := o.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ID != run.ID || got.Mode != ModeQuick {
		t.Errorf("loaded: %+v", got)
	}
}

func TestListRunsMostRecentFirst(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	for i := 0; i < 3; i++ {
		_, _ = o.Run("p", []string{"contrarian"}, ModeQuick)
	}
	runs, err := o.ListRuns(0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("runs: %d", len(runs))
	}
	limit, _ := o.ListRuns(2)
	if len(limit) != 2 {
		t.Errorf("limit: %d", len(limit))
	}
}

func TestStubResponseMentionsPersona(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	run, _ := o.Run("p", []string{"perf-hawk"}, ModeQuick)
	got := run.Rounds[0].Responses["perf-hawk"]
	if !strings.Contains(got, "perf-hawk") {
		t.Errorf("stub response: %q", got)
	}
}

func TestLLMFnInjection(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	o.LLMFn = func(p Persona, proposal string, prior []Round) string {
		return "REAL: " + p.Name + " sees " + proposal
	}
	run, _ := o.Run("ship?", []string{"contrarian"}, ModeQuick)
	got := run.Rounds[0].Responses["contrarian"]
	if !strings.HasPrefix(got, "REAL:") {
		t.Errorf("LLMFn not used: %q", got)
	}
}

func TestSynthesisStubProducesConsensus(t *testing.T) {
	o := NewOrchestrator(t.TempDir())
	run, _ := o.Run("p", nil, ModeQuick)
	if !strings.Contains(run.Consensus, "Council ran") {
		t.Errorf("consensus: %q", run.Consensus)
	}
}
