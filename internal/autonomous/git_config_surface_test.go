// BL407 Phase 5 — config/parity surface: PRD.Git.AutoPR/BaseBranch
// settable via Manager.SetPRDGit, and a daemon-wide DefaultAutoPR
// applied once at PRD creation.
package autonomous

import "testing"

func TestSetPRDGit_SetsBothFields(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	got, err := m.SetPRDGit(prd.ID, true, "develop")
	if err != nil {
		t.Fatalf("SetPRDGit: %v", err)
	}
	if !got.Git.AutoPR || got.Git.BaseBranch != "develop" {
		t.Errorf("got Git=%+v, want AutoPR=true BaseBranch=develop", got.Git)
	}

	persisted, _ := m.Store().GetPRD(prd.ID)
	if !persisted.Git.AutoPR || persisted.Git.BaseBranch != "develop" {
		t.Errorf("persisted Git=%+v, want AutoPR=true BaseBranch=develop", persisted.Git)
	}

	// A second call with different values fully replaces both fields
	// (no partial-merge semantics) — turning AutoPR back off must
	// actually take, not get stuck on because it was true once.
	got2, err := m.SetPRDGit(prd.ID, false, "")
	if err != nil {
		t.Fatalf("SetPRDGit (second call): %v", err)
	}
	if got2.Git.AutoPR || got2.Git.BaseBranch != "" {
		t.Errorf("got2 Git=%+v, want AutoPR=false BaseBranch=empty", got2.Git)
	}
}

func TestSetPRDGit_NotFound(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := m.SetPRDGit("nope", true, ""); err == nil {
		t.Error("expected error for unknown PRD id")
	}
}

func TestCreatePRD_DefaultAutoPR(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultAutoPR = true
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if !prd.Git.AutoPR {
		t.Error("expected Git.AutoPR=true from DefaultAutoPR config, got false")
	}
	persisted, _ := m.Store().GetPRD(prd.ID)
	if !persisted.Git.AutoPR {
		t.Error("DefaultAutoPR was not persisted onto the stored PRD")
	}
}

// Backward compat: DefaultAutoPR false (the zero value, every
// pre-Phase-5 daemon) must not change a new PRD's AutoPR from its
// existing false default.
func TestCreatePRD_NoDefaultAutoPR_LeavesFalse(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if prd.Git.AutoPR {
		t.Error("expected Git.AutoPR=false with no DefaultAutoPR config, got true")
	}
}
