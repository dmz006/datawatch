package autonomous

import (
	"strings"
	"testing"
)

// fakeNode is the test stand-in for a backend node: just its name and the
// Ollama version it reports (as a live probe or node record would supply).
type fakeNode struct {
	name    string
	version string
}

func capCheck(t *testing.T, overrides map[string]string, node fakeNode) CapabilityNodeCheck {
	t.Helper()
	chk, err := CheckCapabilityNode(overrides, CapabilityClassificationVerdict, node.name, node.version)
	if err != nil {
		t.Fatalf("CheckCapabilityNode(%q, %q): %v", node.name, node.version, err)
	}
	return chk
}

// The built-in table holds exactly the documented classification-verdict
// floor: /v1/systemone shipped in Ollama 0.35.0 (doc:
// docs/plans/2026-10-15-logprob-classification-guardrail-design.md,
// section "Backend note: Ollama").
func TestCapabilityMinimumTable(t *testing.T) {
	known := KnownCapabilities()
	if len(known) != 1 || known[0] != CapabilityClassificationVerdict {
		t.Fatalf("known capabilities = %v, want [%s]", known, CapabilityClassificationVerdict)
	}
	min, ok := DefaultMinimumOllama(CapabilityClassificationVerdict)
	if !ok || min != "0.35.0" {
		t.Fatalf("built-in minimum = %q (ok=%v), want 0.35.0", min, ok)
	}
	if _, ok := DefaultMinimumOllama("not_a_capability"); ok {
		t.Fatal("unknown capability should report no built-in minimum")
	}
}

func TestCheckCapabilityNode_BelowMinimum_Refused(t *testing.T) {
	// 0.33.2 is the remote datawatch compute node's version in the design
	// doc: below the 0.35.0 floor.
	chk := capCheck(t, nil, fakeNode{name: "remote-node", version: "0.33.2"})
	if chk.OK {
		t.Fatalf("node below minimum must not pass, got %+v", chk)
	}
	msg := chk.Reason
	for _, want := range []string{"remote-node", "0.33.2", "0.35.0"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message %q must name %q", msg, want)
		}
	}
}

func TestCheckCapabilityNode_EqualMinimum_Passes(t *testing.T) {
	chk := capCheck(t, nil, fakeNode{name: "local-node", version: "0.35.0"})
	if !chk.OK {
		t.Fatalf("node at exactly the minimum must pass: %+v", chk)
	}
}

func TestCheckCapabilityNode_AboveMinimum_Passes(t *testing.T) {
	// 0.40.5 is the local-node version confirmed live in the design doc.
	chk := capCheck(t, nil, fakeNode{name: "local-node", version: "0.40.5"})
	if !chk.OK {
		t.Fatalf("node above the minimum must pass: %+v", chk)
	}
	// Version banners and pre-release suffixes must still parse.
	chk = capCheck(t, nil, fakeNode{name: "banner", version: "Ollama version 0.36.1"})
	if !chk.OK {
		t.Fatalf("bannned version string must parse and pass: %+v", chk)
	}
}

func TestCheckCapabilityNode_UnknownVersion_FailsClosed(t *testing.T) {
	chk := capCheck(t, nil, fakeNode{name: "mystery", version: "N/A"})
	if chk.OK || !chk.Unknown {
		t.Fatalf("unknown version must fail closed: %+v", chk)
	}
	if !strings.Contains(chk.Reason, "mystery") {
		t.Errorf("unknown-version refusal must name the node: %q", chk.Reason)
	}
}

func TestCheckCapabilityNode_UnknownCapability_Errors(t *testing.T) {
	_, err := CheckCapabilityNode(nil, "nope", "node-a", "0.40.5")
	if err == nil || !strings.Contains(err.Error(), "unknown capability") {
		t.Fatalf("err = %v, want unknown-capability error", err)
	}
}

func TestCheckCapabilityNode_Override_RaisesFloor(t *testing.T) {
	overrides := map[string]string{CapabilityClassificationVerdict: "0.40.0"}
	chk := capCheck(t, overrides, fakeNode{name: "local-node", version: "0.35.5"})
	if chk.OK {
		t.Fatalf("0.35.5 must not pass the raised 0.40.0 override: %+v", chk)
	}
	if !chk.Override {
		t.Error("check must report it used the operator override")
	}
	if !strings.Contains(chk.Reason, "0.40.0") {
		t.Errorf("refusal must name the overridden minimum: %q", chk.Reason)
	}
	chk = capCheck(t, overrides, fakeNode{name: "local-node", version: "0.40.5"})
	if !chk.OK {
		t.Fatalf("0.40.5 must pass the raised 0.40.0 override: %+v", chk)
	}
}

func TestCheckCapabilityNode_Override_ClearsFloor(t *testing.T) {
	overrides := map[string]string{CapabilityClassificationVerdict: ""}
	chk := capCheck(t, overrides, fakeNode{name: "remote-node", version: "0.33.2"})
	if !chk.OK {
		t.Fatalf("an empty override clears the floor — 0.33.2 should pass: %+v", chk)
	}
	if !chk.Override {
		t.Error("check must report the override in force")
	}
}

func TestEffectiveMinimumOllama_Precedence(t *testing.T) {
	// Override wins over the built-in table.
	v, _ := EffectiveMinimumOllama(map[string]string{CapabilityClassificationVerdict: "0.9.0"}, CapabilityClassificationVerdict)
	if v != "0.9.0" {
		t.Fatalf("override precedence: got %q, want 0.9.0", v)
	}
	// No override reads the built-in table.
	v, _ = EffectiveMinimumOllama(nil, CapabilityClassificationVerdict)
	if v != "0.35.0" {
		t.Fatalf("built-in fallback: got %q, want 0.35.0", v)
	}
	// Unset capability has no floor.
	if v, ok := EffectiveMinimumOllama(nil, "ghost"); ok {
		t.Fatalf("unknown capability should have no minimum, got %q", v)
	}
}

func TestManager_EnableCapability_Envelope(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "remote-node", "0.33.2"); err == nil {
		t.Fatal("enabling on a below-minimum node must be refused")
	}
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "local-node", "0.40.5"); err != nil {
		t.Fatalf("enabling on an above-minimum node: %v", err)
	}
}

func TestManager_RefusalNamesNodeFoundRequired(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.EnableCapability(CapabilityClassificationVerdict, "stale-edge", "0.34.1")
	if err == nil {
		t.Fatal("want refusal")
	}
	for _, want := range []string{"stale-edge", "0.34.1", "0.35.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q must name %q", err, want)
		}
	}
}

func TestManager_NodeFallsBelowMinimum_FlagsAndFailsClosed(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "edge", "0.40.5"); err != nil {
		t.Fatal(err)
	}
	st, err := m.CapabilityNodeState(CapabilityClassificationVerdict, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if !st.OK || !st.Enabled {
		t.Fatalf("freshly enabled node must be OK: %+v", st)
	}

	// A version change (upgrade or downgrade) leaves the node below the
	// minimum: recheck flags it and the capability fails closed.
	if _, err := m.RecheckCapability(CapabilityClassificationVerdict, "edge", "0.32.9"); err != nil {
		t.Fatal(err)
	}
	st, _ = m.CapabilityNodeState(CapabilityClassificationVerdict, "edge")
	if !st.Flagged || st.OK {
		t.Fatalf("node below minimum after recheck must be flagged and fail closed: %+v", st)
	}
	for _, want := range []string{"edge", "0.32.9", "0.35.0"} {
		if !strings.Contains(st.Reason, want) {
			t.Errorf("flag reason %q must name %q", st.Reason, want)
		}
	}

	// Back to at-or-above the minimum clears the flag.
	if _, err := m.RecheckCapability(CapabilityClassificationVerdict, "edge", "0.35.0"); err != nil {
		t.Fatal(err)
	}
	st, _ = m.CapabilityNodeState(CapabilityClassificationVerdict, "edge")
	if st.Flagged || !st.OK {
		t.Fatalf("recovered node must clear the flag: %+v", st)
	}

	// Unknown version also flags (fail closed).
	if _, err := m.RecheckCapability(CapabilityClassificationVerdict, "edge", "corrupted"); err != nil {
		t.Fatal(err)
	}
	st, _ = m.CapabilityNodeState(CapabilityClassificationVerdict, "edge")
	if !st.Flagged || st.OK {
		t.Fatalf("unknown version must flag the node: %+v", st)
	}

	// Disabled nodes drop out entirely.
	m.DisableCapability(CapabilityClassificationVerdict, "edge")
	st, _ = m.CapabilityNodeState(CapabilityClassificationVerdict, "edge")
	if st.Enabled || st.OK {
		t.Fatalf("disabled node must not be OK: %+v", st)
	}
}

func TestManager_Recheck_UnknownCapability_Errors(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.RecheckCapability("ghost", "edge", "0.40.5"); err == nil {
		t.Fatal("want unknown-capability error")
	}
}

// The report builder: configuration state, gaps, and the automation
// backend minimums.
func reportBackendsOK() AutomationBackends {
	return AutomationBackends{
		Execution:           []string{"pi-prompt"},
		Planning:            []string{"claude-code"},
		Verification:        []string{"claude-code"},
		ClaudeConfigured:    true,
		LocalPoolConfigured: true,
	}
}

func TestRequirementsReport_AllSatisfied(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "local-node", "0.40.5"); err != nil {
		t.Fatal(err)
	}
	rep := m.RequirementsReport(reportBackendsOK())
	if !rep.OK {
		t.Fatalf("report should be OK, missing: %v", rep.Missing)
	}
	if len(rep.Capabilities) != 1 {
		t.Fatalf("capabilities = %d, want 1", len(rep.Capabilities))
	}
	rc := rep.Capabilities[0]
	if rc.Capability != CapabilityClassificationVerdict || rc.MinimumOllama != "0.35.0" || rc.Override {
		t.Fatalf("capability block = %+v", rc)
	}
	if !rc.Satisfied || len(rc.Nodes) != 1 || rc.Nodes[0].Name != "local-node" {
		t.Fatalf("capability nodes = %+v", rc.Nodes)
	}
}

func TestRequirementsReport_CapabilityGap(t *testing.T) {
	// Nothing enabled for the capability → it lands in Missing.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	rep := m.RequirementsReport(reportBackendsOK())
	if rep.OK {
		t.Fatal("report should list the missing capability node")
	}
	found := 0
	for _, miss := range rep.Missing {
		if strings.Contains(miss, CapabilityClassificationVerdict) {
			found++
			if !strings.Contains(miss, "0.35.0") {
				t.Errorf("capability gap %q must cite the minimum", miss)
			}
		}
	}
	if found != 1 {
		t.Fatalf("want exactly one capability gap, missing: %v", rep.Missing)
	}
}

func TestRequirementsReport_OverriddenMinimum(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Requirements = RequirementsConfig{MinOllama: map[string]string{CapabilityClassificationVerdict: "0.40.0"}}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "local-node", "0.40.5"); err != nil {
		t.Fatal(err)
	}
	rep := m.RequirementsReport(reportBackendsOK())
	rc := rep.Capabilities[0]
	if rc.MinimumOllama != "0.40.0" || !rc.Override {
		t.Fatalf("override not reflected: %+v", rc)
	}
	if !rep.OK {
		t.Fatalf("report should be OK, missing: %v", rep.Missing)
	}
}

func TestRequirementsReport_BackendMinimums(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "local-node", "0.40.5"); err != nil {
		t.Fatal(err)
	}
	base := reportBackendsOK()
	cases := []struct {
		name    string
		mutate  func(AutomationBackends) AutomationBackends
		wantSub string
	}{
		{"no execution", func(b AutomationBackends) AutomationBackends { b.Execution = nil; return b }, "execution backend"},
		{"no planning", func(b AutomationBackends) AutomationBackends { b.Planning = nil; return b }, "planning backend"},
		{"no verification", func(b AutomationBackends) AutomationBackends { b.Verification = nil; return b }, "verification backend"},
		{"no provider", func(b AutomationBackends) AutomationBackends { b.ClaudeConfigured = false; b.LocalPoolConfigured = false; return b }, "local model pool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := m.RequirementsReport(tc.mutate(base))
			if rep.OK {
				t.Fatalf("report should fail for %s", tc.name)
			}
			found := false
			for _, miss := range rep.Missing {
				if strings.Contains(miss, tc.wantSub) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing list %v should cite %q", rep.Missing, tc.wantSub)
			}
		})
	}
}

func TestRequirementsReport_EachProviderOptionalButOneRequired(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	// Claude alone is enough.
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "local-node", "0.40.5"); err != nil {
		t.Fatal(err)
	}
	rep2 := m.RequirementsReport(AutomationBackends{
		Execution:          []string{"pi-prompt"},
		Planning:           []string{"claude-code"},
		Verification:       []string{"claude-code"},
		ClaudeConfigured:   true,
		LocalPoolConfigured: false,
	})
	if !rep2.OK {
		t.Fatalf("Claude alone must satisfy the provider rule, missing: %v", rep2.Missing)
	}
	// Local pool alone is enough.
	rep3 := m.RequirementsReport(AutomationBackends{
		Execution:          []string{"pi-prompt"},
		Planning:           []string{"pi-prompt"},
		Verification:       []string{"pi-prompt"},
		ClaudeConfigured:   false,
		LocalPoolConfigured: true,
	})
	if !rep3.OK {
		t.Fatalf("local pool alone must satisfy the provider rule, missing: %v", rep3.Missing)
	}
}

func TestRequirementsReport_FlaggedNodeNotSatisfied(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	if _, err := m.EnableCapability(CapabilityClassificationVerdict, "edge", "0.40.5"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RecheckCapability(CapabilityClassificationVerdict, "edge", "0.0.1"); err != nil {
		t.Fatal(err)
	}
	rep := m.RequirementsReport(reportBackendsOK())
	if rep.Capabilities[0].Satisfied {
		t.Fatal("a flagged (below-minimum) node must not count as satisfying")
	}
	if !rep.Capabilities[0].Nodes[0].Flagged {
		t.Fatal("report node row must show the flag")
	}
}

// The config-file bridge keeps overrides across the yaml → manager round trip.
func TestRequirementsConfig_BridgeAndRoundTrip(t *testing.T) {
	in := RequirementsConfig{MinOllama: map[string]string{CapabilityClassificationVerdict: "0.36.0"}}
	ym := in.yaml()
	back := RequirementsConfigFromYAML(ym)
	if back.MinOllama[CapabilityClassificationVerdict] != "0.36.0" {
		t.Fatalf("bridge round trip lost the override: %+v", back)
	}
	// The config package default ships no overrides (the table is in code).
	d := DefaultRequirementsConfig()
	if _, exists := d.MinOllama[CapabilityClassificationVerdict]; exists {
		t.Fatal("defaults must not hardcode capability minimums")
	}
}
