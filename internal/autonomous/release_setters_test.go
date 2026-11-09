package autonomous

import "testing"

// Manager-level tests for the release size and breaking-change setters: they
// store the value, log one decision each, clear the size on an empty value and
// refuse an unknown size without touching the record.

func TestSetPRDReleaseSize_SetClearAndRefuse(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if err := m.SetPRDReleaseSize(prd.ID, " Minor "); err != nil {
		t.Fatalf("SetPRDReleaseSize: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.ReleaseSize != ReleaseSizeMinor {
		t.Errorf("ReleaseSize = %q, want minor", got.ReleaseSize)
	}
	if d := lastDecision(got, "set_release_size"); d == nil || d.Actor != "operator" || d.Note != "release size derived -> minor" {
		t.Errorf("decision = %+v", d)
	}

	if err := m.SetPRDReleaseSize(prd.ID, "none"); err != nil {
		t.Fatalf("set none: %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.ReleaseSize != ReleaseSizeNone {
		t.Errorf("ReleaseSize = %q, want none (an explicit none is kept)", got.ReleaseSize)
	}

	if err := m.SetPRDReleaseSize(prd.ID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.ReleaseSize != "" {
		t.Errorf("ReleaseSize after clear = %q, want empty", got.ReleaseSize)
	}
	if d := lastDecision(got, "set_release_size"); d == nil || d.Note != "release size none -> derived" {
		t.Errorf("clear decision = %+v", d)
	}

	if err := m.SetPRDReleaseSize(prd.ID, "huge"); err == nil {
		t.Fatal("an unknown release size must be refused")
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.ReleaseSize != "" {
		t.Errorf("refused size must not be stored, got %q", got.ReleaseSize)
	}
	if err := m.SetPRDReleaseSize("nope", "patch"); err == nil {
		t.Error("an unknown Automaton must be refused")
	}
}

func TestSetPRDBreakingChange_SetAndClear(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.SetPRDBreakingChange(prd.ID, true); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if !got.BreakingChange {
		t.Fatal("BreakingChange not stored")
	}
	if d := lastDecision(got, "set_breaking_change"); d == nil || d.Note != "breaking change false -> true" {
		t.Errorf("decision = %+v", d)
	}
	if err := m.SetPRDBreakingChange(prd.ID, false); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.BreakingChange {
		t.Fatal("BreakingChange not cleared")
	}
	if d := lastDecision(got, "set_breaking_change"); d == nil || d.Note != "breaking change true -> false" {
		t.Errorf("clear decision = %+v", d)
	}
	if err := m.SetPRDBreakingChange("nope", true); err == nil {
		t.Error("an unknown Automaton must be refused")
	}
}
