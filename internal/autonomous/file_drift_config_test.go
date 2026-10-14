// BlockOnFileDrift 3-tier precedence tests: PRD > guardrail profile >
// global Config (default false).

package autonomous

import "testing"

func boolPtr(b bool) *bool { return &b }

func newBlockOnFileDriftPRD(t *testing.T, m *Manager) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return prd
}

func TestResolveBlockOnFileDrift_DefaultFalse(t *testing.T) {
	if DefaultConfig().BlockOnFileDrift {
		t.Fatal("DefaultConfig().BlockOnFileDrift should be false (opt-in)")
	}
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	if m.resolveBlockOnFileDrift(newBlockOnFileDriftPRD(t, m)) {
		t.Error("BlockOnFileDrift should resolve false with no overrides")
	}
}

func TestResolveBlockOnFileDrift_GlobalTrue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BlockOnFileDrift = true
	m, _ := NewManager(t.TempDir(), cfg, nil)
	if !m.resolveBlockOnFileDrift(newBlockOnFileDriftPRD(t, m)) {
		t.Error("global BlockOnFileDrift=true should apply when PRD/profile unset")
	}
}

func TestResolveBlockOnFileDrift_ProfileOverridesGlobal(t *testing.T) {
	for _, tc := range []struct {
		global, profile bool
	}{{false, true}, {true, false}} {
		cfg := DefaultConfig()
		cfg.BlockOnFileDrift = tc.global
		m, _ := NewManager(t.TempDir(), cfg, nil)
		p, err := m.CreateGuardrailProfile("strict", "", nil, boolPtr(tc.profile))
		if err != nil {
			t.Fatalf("CreateGuardrailProfile: %v", err)
		}
		prd := newBlockOnFileDriftPRD(t, m)
		prd, err = m.SetPRDGuardrails(prd.ID, p.ID, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("SetPRDGuardrails: %v", err)
		}
		if got := m.resolveBlockOnFileDrift(prd); got != tc.profile {
			t.Errorf("global=%v profile=%v: got %v, want profile value", tc.global, tc.profile, got)
		}
	}
}

func TestResolveBlockOnFileDrift_ProfileUnsetFallsThroughToGlobal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BlockOnFileDrift = true
	m, _ := NewManager(t.TempDir(), cfg, nil)
	p, _ := m.CreateGuardrailProfile("plain", "", nil, nil)
	prd := newBlockOnFileDriftPRD(t, m)
	prd, _ = m.SetPRDGuardrails(prd.ID, p.ID, nil, nil, nil, nil)
	if !m.resolveBlockOnFileDrift(prd) {
		t.Error("profile with nil BlockOnFileDrift should fall through to global true")
	}
}

func TestResolveBlockOnFileDrift_PRDOverrideBeatsProfile(t *testing.T) {
	for _, tc := range []struct {
		profile, prd bool
	}{{true, false}, {false, true}} {
		cfg := DefaultConfig()
		cfg.BlockOnFileDrift = tc.profile
		m, _ := NewManager(t.TempDir(), cfg, nil)
		p, _ := m.CreateGuardrailProfile("p", "", nil, boolPtr(tc.profile))
		prd := newBlockOnFileDriftPRD(t, m)
		prd, err := m.SetPRDGuardrails(prd.ID, p.ID, nil, nil, nil, boolPtr(tc.prd))
		if err != nil {
			t.Fatalf("SetPRDGuardrails: %v", err)
		}
		if got := m.resolveBlockOnFileDrift(prd); got != tc.prd {
			t.Errorf("profile=%v prd=%v: got %v, want PRD value", tc.profile, tc.prd, got)
		}
	}
}

func TestUpdateGuardrailProfile_BlockOnFileDriftNilLeavesUnchanged(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	p, _ := m.CreateGuardrailProfile("p", "", nil, boolPtr(true))
	updated, err := m.UpdateGuardrailProfile(p.ID, "p2", "", nil, nil)
	if err != nil {
		t.Fatalf("UpdateGuardrailProfile: %v", err)
	}
	if updated.BlockOnFileDrift == nil || !*updated.BlockOnFileDrift {
		t.Error("nil blockOnFileDrift on update must leave the existing value unchanged")
	}
	updated, _ = m.UpdateGuardrailProfile(p.ID, "p2", "", nil, boolPtr(false))
	if updated.BlockOnFileDrift == nil || *updated.BlockOnFileDrift {
		t.Error("explicit false on update must be applied")
	}
}
