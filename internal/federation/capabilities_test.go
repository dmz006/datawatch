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
