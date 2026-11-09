package workerprofile

import "testing"

// findEntry returns the entry for scope and key, failing the test when absent.
func findEntry(t *testing.T, entries []InForceEntry, scope, key string) InForceEntry {
	t.Helper()
	for _, e := range entries {
		if e.Scope == scope && e.Key == key {
			return e
		}
	}
	t.Fatalf("no %s entry for %q in %+v", scope, key, entries)
	return InForceEntry{}
}

func TestInForceListsEveryRoleAndKind(t *testing.T) {
	entries := InForce(InForceInput{})
	wantRoles := []string{"worker", "verifier", "judge"}
	wantKinds := []string{"implementation", "docs", "note", "final_gate"}
	if len(entries) != len(wantRoles)+len(wantKinds) {
		t.Fatalf("got %d entries, want %d", len(entries), len(wantRoles)+len(wantKinds))
	}
	for i, role := range wantRoles {
		if entries[i].Scope != ScopeRole || entries[i].Key != role {
			t.Errorf("entry %d = %s/%s, want role/%s", i, entries[i].Scope, entries[i].Key, role)
		}
	}
	for i, kind := range wantKinds {
		e := entries[len(wantRoles)+i]
		if e.Scope != ScopeKind || e.Key != kind {
			t.Errorf("entry %d = %s/%s, want kind/%s", len(wantRoles)+i, e.Scope, e.Key, kind)
		}
	}
}

func TestInForceNothingConfiguredIsNone(t *testing.T) {
	for _, e := range InForce(InForceInput{}) {
		if e.Tier != TierNone {
			t.Errorf("%s/%s tier = %q, want %q", e.Scope, e.Key, e.Tier, TierNone)
		}
		if e.Profile != "" {
			t.Errorf("%s/%s profile = %q, want empty", e.Scope, e.Key, e.Profile)
		}
		if e.Reason == "" {
			t.Errorf("%s/%s has an empty reason", e.Scope, e.Key)
		}
	}
}

func TestInForceConfiguredRoleShowsRoleTier(t *testing.T) {
	entries := InForce(InForceInput{RoleDefaults: map[string]string{"worker": "minimal"}})
	e := findEntry(t, entries, ScopeRole, "worker")
	if e.Tier != "role" {
		t.Errorf("worker tier = %q, want role", e.Tier)
	}
	if e.Profile != "minimal" {
		t.Errorf("worker profile = %q, want minimal", e.Profile)
	}
	if other := findEntry(t, entries, ScopeRole, "judge"); other.Tier != TierNone {
		t.Errorf("judge tier = %q, want %q", other.Tier, TierNone)
	}
}

func TestInForceConfiguredKindShowsKindTier(t *testing.T) {
	entries := InForce(InForceInput{KindDefaults: map[string]string{"docs": "minimal"}})
	e := findEntry(t, entries, ScopeKind, "docs")
	if e.Tier != "kind" {
		t.Errorf("docs tier = %q, want kind", e.Tier)
	}
	if e.Profile != "minimal" {
		t.Errorf("docs profile = %q, want minimal", e.Profile)
	}
	if other := findEntry(t, entries, ScopeKind, "note"); other.Tier != TierNone {
		t.Errorf("note tier = %q, want %q", other.Tier, TierNone)
	}
	if role := findEntry(t, entries, ScopeRole, "worker"); role.Tier != TierNone {
		t.Errorf("worker tier = %q, want %q", role.Tier, TierNone)
	}
}

func TestInForceConfiguredRoleBeatsBuiltInKind(t *testing.T) {
	entries := InForce(InForceInput{RoleDefaults: map[string]string{"worker": "minimal"}})
	e := findEntry(t, entries, ScopeKind, "implementation")
	if e.Tier != "role" || e.Profile != "minimal" {
		t.Errorf("implementation = tier %q profile %q, want role/minimal", e.Tier, e.Profile)
	}
}

func TestInForceGlobalProfileShowsDaemonTier(t *testing.T) {
	entries := InForce(InForceInput{GlobalProfile: "full"})
	for _, e := range entries {
		if e.Tier != TierDaemon {
			t.Errorf("%s/%s tier = %q, want %q", e.Scope, e.Key, e.Tier, TierDaemon)
		}
		if e.Profile != "full" {
			t.Errorf("%s/%s profile = %q, want full", e.Scope, e.Key, e.Profile)
		}
	}
}

func TestInForceUnknownGlobalFallsBackToRole(t *testing.T) {
	none := InForce(InForceInput{GlobalProfile: "no-such-profile"})
	for _, e := range none {
		if e.Tier != TierNone || e.Profile != "" {
			t.Errorf("%s/%s = tier %q profile %q, want none", e.Scope, e.Key, e.Tier, e.Profile)
		}
	}
	withRole := InForce(InForceInput{
		GlobalProfile: "no-such-profile",
		RoleDefaults:  map[string]string{"judge": "minimal"},
	})
	e := findEntry(t, withRole, ScopeRole, "judge")
	if e.Tier != "role" || e.Profile != "minimal" {
		t.Errorf("judge = tier %q profile %q, want role/minimal", e.Tier, e.Profile)
	}
}

func TestInForceUsesLookupForCustomProfiles(t *testing.T) {
	custom := WorkerProfile{Name: "custom", Context: ContextMinimal, MCP: MCPSelection{Mode: MCPNone}}
	lookup := func(name string) (WorkerProfile, bool) {
		if name == "custom" {
			return custom, true
		}
		return Builtin(name)
	}
	entries := InForce(InForceInput{KindDefaults: map[string]string{"note": "custom"}, Lookup: lookup})
	e := findEntry(t, entries, ScopeKind, "note")
	if e.Tier != "kind" || e.Profile != "custom" {
		t.Errorf("note = tier %q profile %q, want kind/custom", e.Tier, e.Profile)
	}
}

func TestInForceTierDefensiveBranches(t *testing.T) {
	// ResolveForLaunch never applies an unconfigured role or kind, so these
	// branches are checked directly with the decisions they would receive.
	if got := inForceTier(LaunchInput{Role: "worker"}, Decision{Layer: string(LayerRole)}, true); got != TierBuiltIn {
		t.Errorf("unconfigured role tier = %q, want %q", got, TierBuiltIn)
	}
	if got := inForceTier(LaunchInput{Role: "worker", Kind: "docs"}, Decision{Layer: string(LayerKind)}, true); got != TierBuiltIn {
		t.Errorf("unconfigured kind tier = %q, want %q", got, TierBuiltIn)
	}
	if got := inForceTier(LaunchInput{Role: "worker", Kind: "docs", KindDefaults: map[string]string{"docs": "minimal"}}, Decision{Layer: string(LayerKind)}, true); got != "kind" {
		t.Errorf("configured kind tier = %q, want kind", got)
	}
	if got := inForceTier(LaunchInput{}, Decision{Layer: string(LayerTask)}, true); got != "task" {
		t.Errorf("task tier = %q, want task", got)
	}
	if got := inForceTier(LaunchInput{}, Decision{Layer: string(LayerRole)}, false); got != TierNone {
		t.Errorf("not applied tier = %q, want %q", got, TierNone)
	}
}
