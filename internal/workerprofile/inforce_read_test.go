package workerprofile

import (
	"strings"
	"testing"
)

// InForceFromConfig reads the live worker profile values from a GET /api/config
// body and returns the same entries as InForce over those values.
func TestInForceFromConfig_MatchesInForce(t *testing.T) {
	body := []byte(`{"autonomous":{"worker_profile":"minimal",` +
		`"worker_profiles":{"mine":{"name":"mine","skills":true}},` +
		`"worker_profile_defaults":{"role":{"judge":"mine"},"kind":{"docs":"digest"}}}}`)
	got, err := InForceFromConfig(body)
	if err != nil {
		t.Fatalf("InForceFromConfig: %v", err)
	}
	want := InForce(InForceInput{
		RoleDefaults:  map[string]string{"judge": "mine"},
		KindDefaults:  map[string]string{"docs": "digest"},
		GlobalProfile: "minimal",
		Lookup: func(name string) (WorkerProfile, bool) {
			if p, ok := Builtin(name); ok {
				return p, true
			}
			if name == "mine" {
				return WorkerProfile{Name: "mine", Skills: true}, true
			}
			return WorkerProfile{}, false
		},
	})
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A body with no worker profile settings still yields one entry per role and
// kind, and a non-JSON body is an error.
func TestInForceFromConfig_EmptyAndBad(t *testing.T) {
	got, err := InForceFromConfig([]byte(`{}`))
	if err != nil || len(got) != len(inForceRoles)+len(inForceKinds) {
		t.Fatalf("empty body: entries=%d err=%v", len(got), err)
	}
	if _, err := InForceFromConfig([]byte(`[`)); err == nil {
		t.Fatal("want an error for a non-JSON body")
	}
}

// FormatInForce prints scope, key, profile (or none) and tier per entry.
func TestFormatInForce(t *testing.T) {
	lines := FormatInForce([]InForceEntry{
		{Scope: ScopeRole, Key: "worker", Profile: "full", Tier: "role"},
		{Scope: ScopeKind, Key: "note", Profile: "", Tier: TierNone},
	})
	want := []string{"role worker profile=full tier=role", "kind note profile=none tier=none"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("FormatInForce = %q, want %q", lines, want)
	}
}
