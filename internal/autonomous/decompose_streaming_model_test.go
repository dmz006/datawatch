// Operator-reported: a PRD configured for claude-code (backend + model +
// decomposition_model all set) kept running decompose against the daemon's
// global planning_model default instead. Root cause: decomposeStreamingCore
// (the function actually wired to the async /decompose REST endpoint used
// by the PWA and API) was a drifted duplicate of Decompose()'s setup logic
// that dropped the per-PRD DecompositionModel tier entirely — it jumped
// straight to the global default whenever one was configured. bl321_decomp_
// priority_test.go covers backend priority for Decompose() only; this file
// covers model priority for the streaming path actually used in production.

package autonomous

import "testing"

func TestDecomposeStreaming_UsesDecompositionModel_OverGlobal(t *testing.T) {
	const perPRDModel = "claude-fable-5-1"
	const globalModel = "ollama/qwen3.8:27b"

	var capturedModel string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedModel = req.Model
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.PlanningModel = globalModel
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/decomp-stream-model", "claude-code", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.SetPRDLLM(prd.ID, "claude-code", "", "claude-sonnet-5", "claude-code", perPRDModel, "test"); err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}

	if _, err := m.DecomposeStreaming(prd.ID, nil); err != nil {
		t.Fatalf("DecomposeStreaming: %v", err)
	}

	if capturedModel != perPRDModel {
		t.Errorf("DecomposeStreaming used model %q, want per-PRD decomposition_model %q (global was %q)",
			capturedModel, perPRDModel, globalModel)
	}
}

func TestDecomposeStreaming_FallsBackToGlobalModel_WhenDecompositionModelEmpty(t *testing.T) {
	const globalModel = "ollama/qwen3.8:27b"

	var capturedModel string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedModel = req.Model
		return fakePlanResp, nil
	}

	cfg := DefaultConfig()
	cfg.PlanningModel = globalModel
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/decomp-stream-model-b", "claude-code", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if prd.DecompositionModel != "" {
		t.Fatalf("expected empty DecompositionModel on new PRD, got %q", prd.DecompositionModel)
	}

	if _, err := m.DecomposeStreaming(prd.ID, nil); err != nil {
		t.Fatalf("DecomposeStreaming: %v", err)
	}

	if capturedModel != globalModel {
		t.Errorf("DecomposeStreaming used model %q, want global fallback %q", capturedModel, globalModel)
	}
}

func TestDecomposeStreaming_FallsBackToPRDModel_WhenDecompositionModelAndGlobalEmpty(t *testing.T) {
	const prdModel = "claude-opus-5"

	var capturedModel string
	fakeFn := func(req DecomposeRequest) (string, error) {
		capturedModel = req.Model
		return fakePlanResp, nil
	}

	cfg := DefaultConfig() // PlanningModel left empty
	m, err := NewManager(t.TempDir(), cfg, fakeFn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/decomp-stream-model-c", "claude-code", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.SetPRDLLM(prd.ID, "claude-code", "", prdModel, "", "", "test"); err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}

	if _, err := m.DecomposeStreaming(prd.ID, nil); err != nil {
		t.Fatalf("DecomposeStreaming: %v", err)
	}

	if capturedModel != prdModel {
		t.Errorf("DecomposeStreaming used model %q, want execution model fallback %q", capturedModel, prdModel)
	}
}
