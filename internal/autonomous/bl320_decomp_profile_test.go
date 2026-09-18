// BL320 — DecompositionProfile on SetPRDLLM: persists field + decision log.
//
// Covers tracker entries for v8.20.0 PRD Split Planning/Execution Backend:
//   - SetPRDLLM persists DecompositionProfile field
//   - Decision log includes decomposition_profile=<value> when non-empty
//   - Empty decomposition_profile is accepted (no error)
//   - Running/completed PRD rejects set_llm

package autonomous

import (
	"strings"
	"testing"
)

func TestBL320_SetPRDLLM_PersistsDecompositionProfile(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/ts-dp", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	updated, err := m.SetPRDLLM(prd.ID, "ollama", "", "qwen3:8b", "my-planning-llm", "", "test")
	if err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}

	if updated.DecompositionProfile != "my-planning-llm" {
		t.Errorf("DecompositionProfile: got %q, want %q", updated.DecompositionProfile, "my-planning-llm")
	}

	// Round-trip: read back from store
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found in store after SetPRDLLM")
	}
	if stored.DecompositionProfile != "my-planning-llm" {
		t.Errorf("stored DecompositionProfile: got %q, want %q", stored.DecompositionProfile, "my-planning-llm")
	}
}

func TestBL320_SetPRDLLM_DecisionLogContainsDecompProfile(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/ts-dp2", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	_, err = m.SetPRDLLM(prd.ID, "ollama", "", "", "opencode", "", "operator")
	if err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}

	stored, _ := m.Store().GetPRD(prd.ID)
	var found bool
	for _, d := range stored.Decisions {
		if strings.Contains(d.Note, "decomposition_profile=opencode") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("decision log should contain 'decomposition_profile=opencode'; decisions: %v", stored.Decisions)
	}
}

func TestBL320_SetPRDLLM_EmptyDecompProfile_Accepted(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/ts-dp3", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	updated, err := m.SetPRDLLM(prd.ID, "ollama", "", "", "", "", "")
	if err != nil {
		t.Fatalf("SetPRDLLM with empty DecompositionProfile should not error: %v", err)
	}
	if updated.DecompositionProfile != "" {
		t.Errorf("DecompositionProfile: got %q, want empty", updated.DecompositionProfile)
	}
}

func TestBL320_SetPRDLLM_EmptyDecisionNote_WhenProfileEmpty(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/ts-dp4", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	_, err = m.SetPRDLLM(prd.ID, "ollama", "", "", "", "", "")
	if err != nil {
		t.Fatalf("SetPRDLLM: %v", err)
	}

	stored, _ := m.Store().GetPRD(prd.ID)
	for _, d := range stored.Decisions {
		if strings.Contains(d.Note, "decomposition_profile=") {
			t.Errorf("decision note should NOT contain decomposition_profile= when profile is empty; got %q", d.Note)
		}
	}
}

func TestBL320_SetPRDLLM_LockedOnRunning(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("test spec", "/tmp/ts-dp5", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	// Manually force PRD to running state
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)

	_, err = m.SetPRDLLM(prd.ID, "ollama", "", "", "", "", "opencode")
	if err == nil {
		t.Error("SetPRDLLM on PRDRunning should return error (LLM overrides locked)")
	}
}
