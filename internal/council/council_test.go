package council

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultPersonasTenCanonicalRoles(t *testing.T) {
	// v6.12.4 — operator added hacker + app-hacker. Total = 12.
	got := DefaultPersonas()
	if len(got) != 12 {
		t.Fatalf("default personas: %d (want 12)", len(got))
	}
	want := map[string]bool{
		"security-skeptic": true, "ux-advocate": true, "perf-hawk": true,
		"simplicity-advocate": true, "ops-realist": true, "contrarian": true,
		"platform-engineer": true, "network-engineer": true,
		"data-architect": true, "privacy": true,
		"hacker": true, "app-hacker": true,
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
	if got := len(o.Personas()); got != 12 {
		t.Errorf("seeded personas: %d (want 12)", got)
	}
}

// v7.0.0 S3 — Council tests now require an InferenceFn since the
// stub fallback is removed (BL295 ASK Q7). Helper builds an
// orchestrator wired to a deterministic mock so the test exercises
// the round/synthesis machinery without a real LLM.
func newTestOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	o := NewOrchestrator(t.TempDir())
	o.LLMRef = "mock"
	o.InferenceFn = func(ctx context.Context, ref, sys, prompt, consumer string) (string, string, error) {
		return "MOCK reply for sysprompt: " + sys, "mock-node", nil
	}
	return o
}

func TestRunQuickModeOneRound(t *testing.T) {
	o := newTestOrchestrator(t)
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
	o := newTestOrchestrator(t)
	run, err := o.Run("proposal", []string{"contrarian"}, ModeDebate)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(run.Rounds) != 3 {
		t.Errorf("rounds: %d (want 3)", len(run.Rounds))
	}
}

func TestRunDefaultsAllPersonas(t *testing.T) {
	o := newTestOrchestrator(t)
	run, err := o.Run("p", nil, ModeQuick)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := len(run.Personas); got != 12 {
		t.Errorf("default personas in run: %d (want 12 — v6.12.4 added 2)", got)
	}
}

func TestRunUnknownPersonaErrors(t *testing.T) {
	o := newTestOrchestrator(t)
	if _, err := o.Run("p", []string{"nope"}, ModeQuick); err == nil {
		t.Error("expected unknown-persona error")
	}
}

func TestRunPersistedAndLoadable(t *testing.T) {
	o := newTestOrchestrator(t)
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
	o := newTestOrchestrator(t)
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

func TestResponseMentionsSystemPrompt(t *testing.T) {
	// v7.0.0 S3 — replaces TestStubResponseMentionsPersona. The mock
	// inference fn echoes the persona's system_prompt so we can verify
	// the orchestrator routes the right prompt to the right persona.
	o := newTestOrchestrator(t)
	run, _ := o.Run("p", []string{"perf-hawk"}, ModeQuick)
	got := run.Rounds[0].Responses["perf-hawk"]
	if !strings.Contains(got, "performance") {
		t.Errorf("response should reference perf-hawk's system_prompt content: %q", got)
	}
}

func TestInferenceFnInjection(t *testing.T) {
	// v7.0.0 S3 — replaces the v6.x LLMFn injection test. The
	// orchestrator now uses InferenceFn (dispatcher closure) rather
	// than the per-persona LLMFn placeholder.
	o := NewOrchestrator(t.TempDir())
	o.LLMRef = "test-llm"
	o.InferenceFn = func(ctx context.Context, ref, sysPrompt, prompt, consumer string) (string, string, error) {
		return "REAL: " + sysPrompt + " sees " + prompt, "gpu-test", nil
	}
	run, err := o.Run("ship?", []string{"contrarian"}, ModeQuick)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := run.Rounds[0].Responses["contrarian"]
	if !strings.HasPrefix(got, "REAL:") {
		t.Errorf("InferenceFn not used: %q", got)
	}
}

func TestSynthesisProducesConsensus(t *testing.T) {
	// v7.0.0 S3 — synthesis is now a real LLM call. Mock returns a
	// labeled CONSENSUS / DISSENT block so we verify splitting works.
	o := NewOrchestrator(t.TempDir())
	o.LLMRef = "mock"
	o.InferenceFn = func(ctx context.Context, ref, sys, prompt, consumer string) (string, string, error) {
		if strings.Contains(sys, "moderator") {
			return "CONSENSUS: All personas agreed.\n\nDISSENT: No material dissent.", "", nil
		}
		return "persona response", "", nil
	}
	run, _ := o.Run("p", nil, ModeQuick)
	if !strings.Contains(run.Consensus, "CONSENSUS") || !strings.Contains(run.Consensus, "agreed") {
		t.Errorf("consensus: %q", run.Consensus)
	}
	if !strings.Contains(run.Dissent, "DISSENT") {
		t.Errorf("dissent: %q", run.Dissent)
	}
}
