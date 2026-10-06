package autonomous

import "testing"

// TestPauseResume (GH#172 D52) covers Manager.Pause/Resume: pausing a
// running PRD flips it to PRDPaused and records a decision; pausing a
// non-running PRD is rejected; resuming a paused PRD flips it back to
// PRDRunning and records a decision; resuming a non-paused PRD is
// rejected; both reject an unknown PRD id.
func TestPauseResume(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	// Pause before running is rejected.
	if _, err := m.Pause(prd.ID, "operator"); err == nil {
		t.Fatal("expected error pausing a non-running PRD, got nil")
	}

	// Force to running, then pause.
	prd, _ = m.store.GetPRD(prd.ID)
	prd.Status = PRDRunning
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	updated, err := m.Pause(prd.ID, "operator")
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if updated.Status != PRDPaused {
		t.Fatalf("Status = %q, want %q", updated.Status, PRDPaused)
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "pause" {
			found = true
		}
	}
	if !found {
		t.Fatal("no pause decision recorded")
	}

	// Pausing again (already paused) is rejected.
	if _, err := m.Pause(prd.ID, "operator"); err == nil {
		t.Fatal("expected error pausing an already-paused PRD, got nil")
	}

	// Resume flips back to running.
	updated, err = m.Resume(prd.ID, "operator")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if updated.Status != PRDRunning {
		t.Fatalf("Status = %q after resume, want %q", updated.Status, PRDRunning)
	}
	found = false
	for _, d := range updated.Decisions {
		if d.Kind == "resume" {
			found = true
		}
	}
	if !found {
		t.Fatal("no resume decision recorded")
	}

	// Resuming again (already running) is rejected.
	if _, err := m.Resume(prd.ID, "operator"); err == nil {
		t.Fatal("expected error resuming an already-running PRD, got nil")
	}

	// Unknown PRD id.
	if _, err := m.Pause("does-not-exist", "operator"); err == nil {
		t.Fatal("expected error pausing unknown prd id, got nil")
	}
	if _, err := m.Resume("does-not-exist", "operator"); err == nil {
		t.Fatal("expected error resuming unknown prd id, got nil")
	}
}
