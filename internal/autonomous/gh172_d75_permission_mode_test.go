package autonomous

import "testing"

// TestSetPermissionMode (GH#172 D75) covers Manager.SetPermissionMode:
// a valid mode persists onto the PRD and records a decision, an empty
// mode clears it back to the session/config default, and an invalid
// mode is rejected without mutating the PRD.
func TestSetPermissionMode(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	updated, err := m.SetPermissionMode(prd.ID, "plan", "operator")
	if err != nil {
		t.Fatalf("SetPermissionMode(plan): %v", err)
	}
	if updated.PermissionMode != "plan" {
		t.Fatalf("PermissionMode = %q, want %q", updated.PermissionMode, "plan")
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "set_permission_mode" {
			found = true
		}
	}
	if !found {
		t.Fatal("no set_permission_mode decision recorded")
	}

	// Empty clears it.
	updated, err = m.SetPermissionMode(prd.ID, "", "operator")
	if err != nil {
		t.Fatalf("SetPermissionMode(\"\"): %v", err)
	}
	if updated.PermissionMode != "" {
		t.Fatalf("PermissionMode = %q after clear, want empty", updated.PermissionMode)
	}

	// Invalid mode rejected, PRD untouched.
	if _, err := m.SetPermissionMode(prd.ID, "not-a-real-mode", "operator"); err == nil {
		t.Fatal("expected error for invalid permission_mode, got nil")
	}
	after, _ := m.store.GetPRD(prd.ID)
	if after.PermissionMode != "" {
		t.Fatalf("PRD mutated by a rejected invalid mode: PermissionMode = %q", after.PermissionMode)
	}

	// Unknown PRD id.
	if _, err := m.SetPermissionMode("does-not-exist", "plan", "operator"); err == nil {
		t.Fatal("expected error for unknown prd id, got nil")
	}
}
