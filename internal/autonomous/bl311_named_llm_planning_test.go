// BL311 — Gap #7: DecompositionBackend with a named registry LLM string.
//
// Verifies that Manager.SetConfig accepts a named LLM string for
// DecompositionBackend and that Config() returns it unchanged.
// This pins the data-flow so regressions (e.g. validation code that
// strips non-adapter-kind strings) are caught immediately.

package autonomous

import (
	"testing"
)

// TestAutonomousManager_PlanningBackend_NamedLLM sets DecompositionBackend
// to a named LLM string (e.g. "ollama-datawatch") and verifies it is stored
// and returned correctly by Config().
func TestAutonomousManager_PlanningBackend_NamedLLM(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	const namedLLM = "ollama-datawatch"

	cfg := m.Config()
	cfg.PlanningBackend = namedLLM
	m.SetConfig(cfg)

	got := m.Config()
	if got.PlanningBackend != namedLLM {
		t.Errorf("DecompositionBackend: got %q, want %q", got.PlanningBackend, namedLLM)
	}
}

// TestAutonomousManager_PlanningBackend_AdapterKindVsNamed verifies that both
// a plain adapter-kind string ("claude-code") and a dotted named LLM string
// ("ollama-datawatch") survive a SetConfig/Config round-trip without
// modification — the manager must not validate or transform these strings.
func TestAutonomousManager_PlanningBackend_AdapterKindVsNamed(t *testing.T) {
	cases := []struct {
		label   string
		backend string
	}{
		{"adapter-kind", "claude-code"},
		{"named-llm", "ollama-datawatch"},
		{"named-with-dots", "my.custom.llm"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}

			cfg := m.Config()
			cfg.PlanningBackend = tc.backend
			m.SetConfig(cfg)

			got := m.Config()
			if got.PlanningBackend != tc.backend {
				t.Errorf("DecompositionBackend round-trip: got %q, want %q",
					got.PlanningBackend, tc.backend)
			}
		})
	}
}

// TestAutonomousManager_PlanningBackend_UsedInDecompose verifies that when
// a PRD has no explicit Backend set, Decompose reads DecompositionBackend
// from the manager config and passes it to the decompose function.
func TestAutonomousManager_PlanningBackend_UsedInDecompose(t *testing.T) {
	const namedLLM = "ollama-datawatch"

	var capturedBackend string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedBackend = req.Backend
		// Return a minimal valid JSON decomposition (the parser expects JSON, not markdown).
		return `{"title":"Test PRD","stories":[{"title":"Story 1","tasks":[{"title":"Task 1","spec":"do the thing"}]}]}`, nil
	}

	cfg := DefaultConfig()
	cfg.PlanningBackend = namedLLM

	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a PRD without an explicit backend so the manager falls back
	// to cfg.PlanningBackend.
	prd, err := m.CreatePRD("build something", t.TempDir(), "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}

	if capturedBackend != namedLLM {
		t.Errorf("Decompose received backend %q, want %q", capturedBackend, namedLLM)
	}
}
