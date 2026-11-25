package autonomous

import (
	"slices"
	"testing"
)

// An Automaton's guardrail_profile and retry_profile fields are documented as
// profile names, while profiles are stored under a short random id. Every
// profile-tier resolution must find the profile by name as well as by id.

func namedProfileFixture(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	yes, no := true, false
	if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{
		ID: "random-id-1", Name: "named-guard", Guardrails: []string{"security"},
		NotifyEvents: []string{"prd.failed", "prd.completed"}, BlockOnFileDrift: &yes,
	}); err != nil {
		t.Fatalf("SaveGuardrailProfile: %v", err)
	}
	if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{ID: "random-id-3", Name: "named-guard-off", BlockOnFileDrift: &no}); err != nil {
		t.Fatalf("SaveGuardrailProfile: %v", err)
	}
	if err := m.Store().SaveRetryProfile(&RetryProfile{
		ID: "random-id-2", Name: "named-retry", Budgets: map[string]int{"infra": 7},
		CeilingTotal: 11, InfraRetryEscalateAfter: 6,
		ContentAutoFixEnabled: &no, AutoFixSkipApproval: &no,
	}); err != nil {
		t.Fatalf("SaveRetryProfile: %v", err)
	}
	return m
}

func TestGuardrailProfileAssignedByNameOrID(t *testing.T) {
	m := namedProfileFixture(t)
	for _, key := range []string{"named-guard", "random-id-1"} {
		prd := &PRD{ID: "a", GuardrailProfile: key}
		if got := m.resolveGuardrails(prd, "task"); !slices.Equal(got, []string{"security"}) {
			t.Errorf("%s: guardrails = %v, want [security]", key, got)
		}
		got := m.ResolveNotifyEvents(prd)
		if got.Source != NotifySourceProfile || !slices.Equal(got.Events, []string{"prd.failed", "prd.completed"}) {
			t.Errorf("%s: notify events = %v from %q, want the profile's", key, got.Events, got.Source)
		}
		if !m.resolveBlockOnFileDrift(prd) {
			t.Errorf("%s: block_on_file_drift = false, want the profile's true", key)
		}
	}
	if !m.driftBlockExplicitlyOff(&PRD{ID: "a", GuardrailProfile: "named-guard-off"}) {
		t.Errorf("a named profile's block_on_file_drift=false was not read as an opt-out")
	}
	if m.resolveBlockOnFileDrift(&PRD{ID: "a", GuardrailProfile: "no-such-profile"}) {
		t.Errorf("an unknown profile must fall through to the global default")
	}
}

func TestRetryProfileAssignedByNameOrID(t *testing.T) {
	m := namedProfileFixture(t)
	for _, key := range []string{"named-retry", "random-id-2"} {
		prd := &PRD{ID: "a", RetryProfile: key}
		if got := m.resolveRetryPolicy(prd, "infra"); got != 7 {
			t.Errorf("%s: infra budget = %d, want 7", key, got)
		}
		if got := m.resolveRetryCeiling(prd); got != 11 {
			t.Errorf("%s: ceiling = %d, want 11", key, got)
		}
		if got := m.resolveInfraEscalateAfter(prd); got != 6 {
			t.Errorf("%s: escalate after = %d, want 6", key, got)
		}
		if m.resolveContentAutoFixEnabled(prd) {
			t.Errorf("%s: content auto fix = true, want the profile's false", key)
		}
		if m.resolveAutoFixSkipApproval(prd) {
			t.Errorf("%s: auto fix skip approval = true, want the profile's false", key)
		}
	}
}
