// BL303 S2 — guardrail registry, profile CRUD, and per-Automaton override tests.

package autonomous

import (
	"testing"
)

// ── library ───────────────────────────────────────────────────────────────

func TestGuardrailLibrary_BuiltInsPresent(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	lib := m.GuardrailLibrary()
	names := make(map[string]bool)
	for _, e := range lib {
		names[e.Name] = true
	}
	for _, want := range []string{"sast-scan", "secrets-scan", "deps-scan"} {
		if !names[want] {
			t.Errorf("built-in guardrail %q missing from library", want)
		}
	}
}

func TestRegisterGuardrail_AppearsInLibrary(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	m.RegisterGuardrail(GuardrailEntry{
		Name:        "my-custom",
		Description: "A custom guardrail",
		Type:        "custom",
	})
	lib := m.GuardrailLibrary()
	for _, e := range lib {
		if e.Name == "my-custom" {
			return
		}
	}
	t.Fatal("registered guardrail not found in library")
}

// ── profiles ──────────────────────────────────────────────────────────────

func TestGuardrailProfileCRUD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)

	// Create.
	p, err := m.CreateGuardrailProfile("strict", "All scans", []string{"sast-scan", "secrets-scan"})
	if err != nil {
		t.Fatalf("CreateGuardrailProfile: %v", err)
	}
	if p.Name != "strict" || len(p.Guardrails) != 2 {
		t.Fatalf("profile mismatch: %+v", p)
	}

	// Get.
	got, ok := m.GetGuardrailProfile(p.ID)
	if !ok {
		t.Fatalf("GetGuardrailProfile not found after create")
	}
	if got.ID != p.ID {
		t.Errorf("ID mismatch: %q vs %q", got.ID, p.ID)
	}

	// List.
	list := m.ListGuardrailProfiles()
	if len(list) != 1 {
		t.Fatalf("ListGuardrailProfiles: want 1, got %d", len(list))
	}

	// Update.
	updated, err := m.UpdateGuardrailProfile(p.ID, "medium", "One scan", []string{"sast-scan"})
	if err != nil {
		t.Fatalf("UpdateGuardrailProfile: %v", err)
	}
	if updated.Name != "medium" || len(updated.Guardrails) != 1 {
		t.Errorf("updated mismatch: %+v", updated)
	}

	// Delete.
	if err := m.DeleteGuardrailProfile(p.ID); err != nil {
		t.Fatalf("DeleteGuardrailProfile: %v", err)
	}
	if _, ok := m.GetGuardrailProfile(p.ID); ok {
		t.Error("profile still found after delete")
	}
	if len(m.ListGuardrailProfiles()) != 0 {
		t.Error("list not empty after delete")
	}
}

func TestUpdateGuardrailProfile_NotFound(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	_, err := m.UpdateGuardrailProfile("nope", "x", "", nil)
	if err == nil {
		t.Fatal("expected error for unknown profile ID")
	}
}

func TestDeleteGuardrailProfile_NotFound(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err := m.DeleteGuardrailProfile("nope"); err == nil {
		t.Fatal("expected error for unknown profile ID")
	}
}

// ── per-Automaton override ────────────────────────────────────────────────

func TestSetPRDGuardrails_ExplicitFields(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)

	updated, err := m.SetPRDGuardrails(prd.ID, "", []string{"sast-scan"}, []string{"deps-scan"})
	if err != nil {
		t.Fatalf("SetPRDGuardrails: %v", err)
	}
	if len(updated.PerTaskGuardrails) != 1 || updated.PerTaskGuardrails[0] != "sast-scan" {
		t.Errorf("per_task_guardrails wrong: %v", updated.PerTaskGuardrails)
	}
	if len(updated.PerStoryGuardrails) != 1 || updated.PerStoryGuardrails[0] != "deps-scan" {
		t.Errorf("per_story_guardrails wrong: %v", updated.PerStoryGuardrails)
	}
}

func TestSetPRDGuardrails_NamedProfile(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)

	updated, err := m.SetPRDGuardrails(prd.ID, "strict-profile", nil, nil)
	if err != nil {
		t.Fatalf("SetPRDGuardrails: %v", err)
	}
	if updated.GuardrailProfile != "strict-profile" {
		t.Errorf("guardrail_profile = %q, want strict-profile", updated.GuardrailProfile)
	}
}

func TestSetPRDGuardrails_NotFound(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	_, err := m.SetPRDGuardrails("nope", "", nil, nil)
	if err == nil {
		t.Fatal("expected error for missing PRD")
	}
}

// ── resolveGuardrails ─────────────────────────────────────────────────────

func TestResolveGuardrails_ExplicitBeatsProfile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"global-g"}
	m, _ := NewManager(t.TempDir(), cfg, nil)

	// Create a profile.
	p, _ := m.CreateGuardrailProfile("p", "", []string{"profile-g"})

	// PRD with explicit + profile.
	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	prd.GuardrailProfile = p.ID
	prd.PerTaskGuardrails = []string{"explicit-g"}
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)

	result := m.resolveGuardrails(prd, "task")
	if len(result) != 1 || result[0] != "explicit-g" {
		t.Errorf("explicit should beat profile+global, got: %v", result)
	}
}

func TestResolveGuardrails_ProfileBeatsGlobal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"global-g"}
	m, _ := NewManager(t.TempDir(), cfg, nil)

	p, _ := m.CreateGuardrailProfile("p", "", []string{"profile-g"})

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	prd.GuardrailProfile = p.ID
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)

	result := m.resolveGuardrails(prd, "task")
	if len(result) != 1 || result[0] != "profile-g" {
		t.Errorf("profile should beat global, got: %v", result)
	}
}

func TestResolveGuardrails_GlobalFallback(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"global-g"}
	m, _ := NewManager(t.TempDir(), cfg, nil)

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)

	result := m.resolveGuardrails(prd, "task")
	if len(result) != 1 || result[0] != "global-g" {
		t.Errorf("should fall back to global, got: %v", result)
	}
}

// ── store persistence ─────────────────────────────────────────────────────

func TestGuardrailProfilePersistence(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)

	p, _ := m.CreateGuardrailProfile("saved", "persist test", []string{"sast-scan"})

	// Reload from disk.
	m2, _ := NewManager(dir, DefaultConfig(), nil)
	got, ok := m2.GetGuardrailProfile(p.ID)
	if !ok {
		t.Fatal("profile not found after reload")
	}
	if got.Name != "saved" || len(got.Guardrails) != 1 {
		t.Errorf("profile data wrong after reload: %+v", got)
	}
}
