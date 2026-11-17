package autonomous

import (
	"slices"
	"testing"
)

// Named profile resolution regression tests.
//
// The operator assigns a profile to an Automaton by its NAME (the stored
// field holds whatever they typed), but profiles are persisted keyed by a
// short random ID. A lookup against the store's ID key alone always missed
// and every profile-tier resolution silently fell through to the global
// tier (the "profile tier resolved as source=global" failure). These tests
// pin the name: lookup must hit by ID and by NAME, and miss for a profile
// that is not in the store at all.

func TestPRDNamedProfile_LookupsByIDAndName(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// ID ≠ Name on purpose: production IDs are random ("d8f3a21c"), the
	// Name is the human label the operator assigns by.
	if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{
		ID:           "id-guard",
		Name:         "name-guard",
		NotifyEvents: []string{"prd.failed"},
	}); err != nil {
		t.Fatalf("SaveGuardrailProfile: %v", err)
	}
	if err := m.Store().SaveRetryProfile(&RetryProfile{
		ID:           "id-retry",
		Name:         "name-retry",
		Budgets:      map[string]int{"infra": 7},
		CeilingTotal: 9,
	}); err != nil {
		t.Fatalf("SaveRetryProfile: %v", err)
	}

	cases := []struct {
		key  string
		want bool
	}{
		{key: "id-guard", want: true},
		{key: "name-guard", want: true},
		{key: "no-such-profile", want: false},
		{key: "", want: false},
	}
	for _, tc := range cases {
		if p, ok := m.prdGuardrailProfile(tc.key); ok != tc.want {
			t.Errorf("prdGuardrailProfile(%q) ok=%v (profile=%+v), want ok=%v", tc.key, ok, p, tc.want)
		}
		retryWant := tc.key == "id-retry" || tc.key == "name-retry"
		if p, ok := m.prdRetryProfile(tc.key); ok != retryWant {
			t.Errorf("prdRetryProfile(%q) ok=%v (profile=%+v), want ok=%v", tc.key, ok, p, retryWant)
		}
	}

	// Spot-check a hit returns the right profile, not a different one.
	if p, ok := m.prdRetryProfile("name-retry"); !ok || p.CeilingTotal != 9 || p.Budgets["infra"] != 7 {
		t.Fatalf("prdRetryProfile(\"name-retry\") = %v, ok=%v — want the name-retry profile", p, ok)
	}
}

func TestResolveNotifyEvents_ProfileReferencedByName(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{
		ID:           "random-id-1",
		Name:         "smoke-notify-events-named",
		NotifyEvents: []string{"prd.failed", "prd.completed"},
	}); err != nil {
		t.Fatalf("SaveGuardrailProfile: %v", err)
	}
	// The Automaton references the profile by its NAME, with an empty
	// Automaton tier, so the profile tier is the one that should resolve.
	prd := &PRD{ID: "auto-by-name", GuardrailProfile: "smoke-notify-events-named"}
	got := m.ResolveNotifyEvents(prd)
	if got.Source != NotifySourceProfile {
		t.Fatalf("Source = %q, want %q (events=%v)", got.Source, NotifySourceProfile, got.Events)
	}
	if !slices.Equal(got.Events, []string{"prd.failed", "prd.completed"}) {
		t.Fatalf("Events = %v, want the profile's set", got.Events)
	}
}

func TestResolveRetryPolicy_ProfileReferencedByName(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	enabled := true
	if err := m.Store().SaveRetryProfile(&RetryProfile{
		ID:                      "random-id-2",
		Name:                    "named-retry-profile",
		Budgets:                 map[string]int{"infra": 7},
		CeilingTotal:            11,
		InfraRetryEscalateAfter: 5,
		ContentAutoFixEnabled:   &enabled,
	}); err != nil {
		t.Fatalf("SaveRetryProfile: %v", err)
	}
	prd := &PRD{ID: "auto-by-name", RetryProfile: "named-retry-profile"}

	if got := m.resolveRetryPolicy(prd, "infra"); got != 7 {
		t.Fatalf("resolveRetryPolicy(infra) = %d, want 7 from the named profile", got)
	}
	if got := m.resolveRetryCeiling(prd); got != 11 {
		t.Fatalf("resolveRetryCeiling() = %d, want 11 from the named profile", got)
	}
	if got := m.resolveInfraEscalateAfter(prd); got != 5 {
		t.Fatalf("resolveInfraEscalateAfter() = %d, want 5 from the named profile", got)
	}
	if got := m.resolveContentAutoFixEnabled(prd); got != true {
		t.Fatalf("resolveContentAutoFixEnabled() = %v, want true from the named profile", got)
	}
}
