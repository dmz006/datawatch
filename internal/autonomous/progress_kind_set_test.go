package autonomous

import "testing"

func TestProgressKindSet_StoresKindAndLogsDecision(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	revBefore, _ := m.Store().PRDRev(prd.ID)

	if err := m.SetPRDKind(prd.ID, " Bug "); err != nil {
		t.Fatalf("SetPRDKind: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Kind != PRDKindBug {
		t.Errorf("Kind = %q, want %q", got.Kind, PRDKindBug)
	}
	if rev, _ := m.Store().PRDRev(prd.ID); rev != revBefore+1 {
		t.Errorf("Rev = %d, want %d", rev, revBefore+1)
	}
	if d := lastDecision(got, "set_kind"); d == nil || d.Actor != "operator" || d.Note != "kind derived -> bug" {
		t.Errorf("decision = %+v", d)
	}

	if err := m.SetPRDKind(prd.ID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.Kind != "" {
		t.Errorf("Kind after clear = %q, want empty", got.Kind)
	}
	if d := lastDecision(got, "set_kind"); d == nil || d.Note != "kind bug -> derived" {
		t.Errorf("clear decision = %+v", d)
	}
}

func TestProgressKindSet_RejectsUnknownKindWithoutChange(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	revBefore, _ := m.Store().PRDRev(prd.ID)
	if err := m.SetPRDKind(prd.ID, "chore"); err == nil {
		t.Fatal("expected an error for an unknown kind")
	}
	if rev, _ := m.Store().PRDRev(prd.ID); rev != revBefore {
		t.Errorf("a refused kind must not save: Rev %d -> %d", revBefore, rev)
	}
}

func TestProgressKindSet_UnknownID(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.SetPRDKind("nope", "bug"); err == nil {
		t.Fatal("expected an error for an unknown id")
	}
}
