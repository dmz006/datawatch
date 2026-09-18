// BL321 — Decompose() backend priority: DecompositionProfile wins over global PlanningBackend.
//
// Covers tracker entries for v8.20.0:
//   - Decompose() uses DecompositionProfile when set (priority over global)
//   - Decompose() falls back to global PlanningBackend when profile is empty

package autonomous

import (
	"testing"
)

// fakePlanResp is a minimal JSON decomposition response for tests.
const fakePlanResp = `{"title":"Test PRD","stories":[{"title":"Story 1","tasks":[{"title":"Task 1","spec":"do it"}]}]}`

func TestBL321_Decompose_UsesDecompositionProfile_OverGlobal(t *testing.T) {
	const perPRDProfile = "per-prd-planner"
	const globalBackend = "global-planner"

	var capturedBackend string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedBackend = req.Backend
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.PlanningBackend = globalBackend
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a PRD and assign a per-PRD decomposition_profile
	prd, err := m.CreatePRD("test spec", "/tmp/bl321", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_, err = m.SetPRDLLM(prd.ID, "ollama", "", "", perPRDProfile, "", "test")
	if err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}

	if capturedBackend != perPRDProfile {
		t.Errorf("Decompose used backend %q, want per-PRD profile %q (global=%q)",
			capturedBackend, perPRDProfile, globalBackend)
	}
}

func TestBL321_Decompose_FallsBackToGlobal_WhenProfileEmpty(t *testing.T) {
	const globalBackend = "global-planner-fallback"

	var capturedBackend string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedBackend = req.Backend
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.PlanningBackend = globalBackend
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create PRD with no per-PRD profile
	prd, err := m.CreatePRD("test spec", "/tmp/bl321b", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if prd.DecompositionProfile != "" {
		t.Fatalf("expected empty DecompositionProfile on new PRD, got %q", prd.DecompositionProfile)
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}

	if capturedBackend != globalBackend {
		t.Errorf("Decompose used backend %q, want global fallback %q", capturedBackend, globalBackend)
	}
}

func TestBL321_Decompose_PRDProfileOverridesGlobal_BothNonEmpty(t *testing.T) {
	const perPRD = "per-prd-custom"
	const global = "global-config-value"

	var captured string
	cfg := DefaultConfig()
	cfg.PlanningBackend = global
	m, err := NewManager(t.TempDir(), cfg, func(req DecomposeRequest) (string, error) {
		captured = req.Backend
		return fakePlanResp, nil
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", "/tmp/bl321c", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.SetPRDLLM(prd.ID, "ollama", "", "", perPRD, "", "test"); err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}

	if captured != perPRD {
		t.Errorf("Decompose used %q, want per-PRD profile %q (global was %q)", captured, perPRD, global)
	}
}
