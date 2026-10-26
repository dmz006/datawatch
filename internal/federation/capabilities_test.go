package federation_test

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestResolve_IndividualCap(t *testing.T) {
	got := federation.Resolve([]string{federation.CapSessionsList}, nil)
	if len(got) != 1 || got[0] != federation.CapSessionsList {
		t.Fatalf("expected [%s], got %v", federation.CapSessionsList, got)
	}
}

func TestResolve_BuiltinGroup(t *testing.T) {
	got := federation.Resolve([]string{"monitor"}, nil)
	if len(got) == 0 {
		t.Fatal("expected non-empty resolved set for 'monitor'")
	}
	hasCap := func(c string) bool {
		for _, g := range got {
			if g == c {
				return true
			}
		}
		return false
	}
	if !hasCap(federation.CapHealthRead) {
		t.Errorf("monitor should include %s", federation.CapHealthRead)
	}
	if !hasCap(federation.CapUsageRead) {
		t.Errorf("monitor should include %s", federation.CapUsageRead)
	}
	if !hasCap(federation.CapSessionsList) {
		t.Errorf("monitor should include %s", federation.CapSessionsList)
	}
	if hasCap(federation.CapSessionsWrite) {
		t.Errorf("monitor should NOT include %s", federation.CapSessionsWrite)
	}
}

func TestResolve_MixedGroupAndCap(t *testing.T) {
	got := federation.Resolve([]string{"monitor", federation.CapSessionsInput}, nil)
	has := map[string]bool{}
	for _, c := range got {
		has[c] = true
	}
	if !has[federation.CapHealthRead] {
		t.Error("should have health:read from monitor")
	}
	if !has[federation.CapSessionsInput] {
		t.Error("should have sessions:input from direct cap")
	}
}

func TestResolve_Dedup(t *testing.T) {
	// Passing the same cap twice should deduplicate.
	got := federation.Resolve([]string{federation.CapSessionsList, federation.CapSessionsList}, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d", len(got))
	}
}

func TestResolve_CustomGroup(t *testing.T) {
	custom := map[string]*federation.CapabilityGroup{
		"my-group": {
			Name: "my-group",
			Caps: []string{federation.CapSecretsRead, federation.CapDocsRead},
		},
	}
	got := federation.Resolve([]string{"my-group"}, custom)
	has := map[string]bool{}
	for _, c := range got {
		has[c] = true
	}
	if !has[federation.CapSecretsRead] {
		t.Error("custom group should include secrets:read")
	}
	if !has[federation.CapDocsRead] {
		t.Error("custom group should include docs:read")
	}
}

func TestResolve_CycleGuard(t *testing.T) {
	// Self-referencing group should not loop.
	custom := map[string]*federation.CapabilityGroup{
		"cyclic": {
			Name: "cyclic",
			Caps: []string{"cyclic", federation.CapHealthRead},
		},
	}
	got := federation.Resolve([]string{"cyclic"}, custom)
	if len(got) == 0 {
		t.Error("expected at least health:read even with cyclic group")
	}
}

func TestCheck(t *testing.T) {
	granted := []string{federation.CapSessionsList, federation.CapHealthRead}
	if !federation.Check(granted, federation.CapSessionsList) {
		t.Error("Check should return true for granted cap")
	}
	if federation.Check(granted, federation.CapSessionsWrite) {
		t.Error("Check should return false for missing cap")
	}
}

func TestFullControl_HasAll(t *testing.T) {
	got := federation.Resolve([]string{"full-control"}, nil)
	has := map[string]bool{}
	for _, c := range got {
		has[c] = true
	}
	must := []string{
		federation.CapSessionsList, federation.CapSessionsWrite, federation.CapSessionsKill,
		federation.CapAgentsSpawn, federation.CapLLMsWrite, federation.CapComputeWrite,
		federation.CapSecretsWrite, federation.CapFederationWrite,
		// Design A2 audit (v8.39.24) additions — handleQueue and
		// handleResultStore had no capability of any kind to grant until
		// these were added.
		federation.CapQueueRead, federation.CapQueueWrite,
		federation.CapResultsList, federation.CapResultsRead, federation.CapResultsWrite,
		federation.CapPluginRead, federation.CapPluginWrite,
		// Federated usage calculator series.
		federation.CapUsageRead, federation.CapUsageWrite,
	}
	for _, c := range must {
		if !has[c] {
			t.Errorf("full-control should include %s", c)
		}
	}
}

// TestFederationPeer_DefaultCaps covers the SEC-009 (v9.0.0, breaking)
// narrowing: a newly registered peer's default group used to grant broad
// read access (sessions, agents, observers, alerts, dashboard, and every
// OTHER peer's entry via federation:list/read) — narrowed to just
// health:read + federation:self (its own entry only). A peer that
// genuinely needs more must be granted a custom group explicitly.
func TestFederationPeer_DefaultCaps(t *testing.T) {
	got := federation.Resolve([]string{"federation-peer"}, nil)
	has := map[string]bool{}
	for _, c := range got {
		has[c] = true
	}
	if !has[federation.CapHealthRead] {
		t.Error("federation-peer should have health:read")
	}
	if !has[federation.CapFederationSelf] {
		t.Error("federation-peer should have federation:self")
	}
	// Nothing broader is granted by default anymore.
	noLonger := []string{
		federation.CapSessionsList, federation.CapSessionsInput, federation.CapSessionsRead,
		federation.CapAgentsList, federation.CapAgentsRead,
		federation.CapObserversList, federation.CapObserversRead,
		federation.CapAlertsList, federation.CapAlertsRead,
		federation.CapDashboardRead,
		federation.CapFederationList, federation.CapFederationRead,
		federation.CapUsageRead, federation.CapUsageWrite,
	}
	for _, c := range noLonger {
		if has[c] {
			t.Errorf("federation-peer should NOT have %s by default (SEC-009 narrowed this) — grant it via a custom group instead", c)
		}
	}
	// Still never write or secrets.
	if has[federation.CapSessionsWrite] {
		t.Error("federation-peer should NOT have sessions:write")
	}
	if has[federation.CapSecretsRead] {
		t.Error("federation-peer should NOT have secrets:read")
	}
	if has[federation.CapFederationWrite] {
		t.Error("federation-peer should NOT have federation:write")
	}
}

func TestListBuiltinGroups(t *testing.T) {
	groups := federation.ListBuiltinGroups()
	if len(groups) == 0 {
		t.Fatal("expected non-empty builtin group list")
	}
	found := false
	for _, g := range groups {
		if g.Name == "federation-peer" {
			found = true
		}
		if !g.Builtin {
			t.Errorf("builtin group %q has Builtin=false", g.Name)
		}
	}
	if !found {
		t.Error("expected 'federation-peer' in builtin group list")
	}
}

// TestCapSessionsResultWrite_NotInAnyBuiltinGroup (GH#204 Phase 1) —
// sessions:result_write is a per-session, explicitly minted capability: it
// must not leak into any built-in group's default set (not even
// full-control), so a peer or a session granted a group never receives it
// without the daemon minting it into that session's own scoped token.
func TestCapSessionsResultWrite_NotInAnyBuiltinGroup(t *testing.T) {
	for _, g := range federation.ListBuiltinGroups() {
		resolved := federation.Resolve([]string{g.Name}, nil)
		if len(resolved) == 0 {
			t.Fatalf("builtin group %q resolved to no caps at all", g.Name)
		}
		if federation.Check(resolved, federation.CapSessionsResultWrite) {
			t.Errorf("builtin group %q must not grant %s by default — it is minted explicitly into per-session tokens", g.Name, federation.CapSessionsResultWrite)
		}
	}
	// And a standalone grant still resolves (the constant is live).
	if got := federation.Resolve([]string{federation.CapSessionsResultWrite}, nil); len(got) != 1 || got[0] != federation.CapSessionsResultWrite {
		t.Errorf("standalone grant of %s should resolve to itself, got %v", federation.CapSessionsResultWrite, got)
	}
}

// TestCapFederationFingerprintOnlyInFullControl — federation:fingerprint
// gates the endpoint a peer calls to read this daemon's fingerprint hashes.
// It must be held by full-control (allCaps) but by NO other built-in group
// — a peer's control capability does not imply fingerprint trust (the
// per-peer opt-in is the Entry's FingerprintSync flag, a separate
// decision; docs/plans/2026-10-14-concurrency-and-budget-design.md, open
// question 10). A standalone grant still resolves so it can be given to a
// custom group explicitly.
func TestCapFederationFingerprintOnlyInFullControl(t *testing.T) {
	for _, g := range federation.ListBuiltinGroups() {
		resolved := federation.Resolve([]string{g.Name}, nil)
		if len(resolved) == 0 {
			t.Fatalf("builtin group %q resolved to no caps at all", g.Name)
		}
		has := federation.Check(resolved, federation.CapFederationFingerprint)
		if g.Name == "full-control" {
			if !has {
				t.Errorf("full-control must hold %s", federation.CapFederationFingerprint)
			}
			continue
		}
		if has {
			t.Errorf("builtin group %q must NOT grant %s by default — it is opt-in per peer (FingerprintSync) and held only by full-control among builtins", g.Name, federation.CapFederationFingerprint)
		}
	}
	// A standalone grant still resolves (the constant is live and grantable
	// via a custom group).
	if got := federation.Resolve([]string{federation.CapFederationFingerprint}, nil); len(got) != 1 || got[0] != federation.CapFederationFingerprint {
		t.Errorf("standalone grant of %s should resolve to itself, got %v", federation.CapFederationFingerprint, got)
	}
}

// TestPluginCaps_NotInDefaultPresets: plugin:read / plugin:write must be
// granted explicitly — no built-in group other than full-control carries them.
func TestPluginCaps_NotInDefaultPresets(t *testing.T) {
	for _, g := range federation.ListBuiltinGroups() {
		if g.Name == "full-control" {
			continue
		}
		for _, c := range federation.Resolve([]string{g.Name}, nil) {
			if c == federation.CapPluginRead || c == federation.CapPluginWrite {
				t.Errorf("builtin group %q should NOT include %s", g.Name, c)
			}
		}
	}
}

// TestUsageCapabilityGroups — the shared usage calculator series splits
// into usage:read (a peer reading the keeper's readings) and usage:write
// (a peer forwarding writes to the keeper). Presets grant them the same
// way as their read/write ancestors: read-only presets grant read, the
// config + session operational grants grant both, and the minimal peer
// default grants neither.
func TestUsageCapabilityGroups(t *testing.T) {
	cases := []struct {
		group    string
		required string
		want     bool
	}{
		{"monitor", federation.CapUsageRead, true},
		{"monitor", federation.CapUsageWrite, false},
		{"read-only", federation.CapUsageRead, true},
		{"read-only", federation.CapUsageWrite, false},
		{"analytics-viewer", federation.CapUsageRead, true},
		{"analytics-viewer", federation.CapUsageWrite, false},
		{"config-admin", federation.CapUsageRead, true},
		{"config-admin", federation.CapUsageWrite, true},
		{"session-default", federation.CapUsageRead, true},
		{"session-default", federation.CapUsageWrite, true},
		{"session-viewer", federation.CapUsageRead, false},
		{"session-viewer", federation.CapUsageWrite, false},
		{"federation-peer", federation.CapUsageRead, false},
		{"federation-peer", federation.CapUsageWrite, false},
		{"full-control", federation.CapUsageRead, true},
		{"full-control", federation.CapUsageWrite, true},
	}
	for _, tc := range cases {
		granted := federation.Resolve([]string{tc.group}, nil)
		if got := federation.Check(granted, tc.required); got != tc.want {
			t.Errorf("%s: %s = %v, want %v", tc.group, tc.required, got, tc.want)
		}
	}
}
