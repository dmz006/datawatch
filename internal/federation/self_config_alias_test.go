package federation_test

// BL412 option C followup — mcp.allow_self_config is the alias for the
// builtin "self-config" capability group, and the session-default grant
// for spawned sessions' scoped credentials carries that group when the
// alias is enabled (the default).

import (
	"slices"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestSessionDefaultGrantCaps_AliasEnabled(t *testing.T) {
	grant := federation.SessionDefaultGrantCaps(true)
	if !slices.Contains(grant, "session-default") {
		t.Errorf("enabled grant must carry session-default, got %v", grant)
	}
	if !slices.Contains(grant, federation.SelfConfigGroup) {
		t.Errorf("enabled grant must carry the %q group, got %v", federation.SelfConfigGroup, grant)
	}

	resolved := federation.Resolve(grant, nil)
	if !federation.Check(resolved, federation.CapConfigWrite) {
		t.Errorf("alias enabled: resolved grant must include %s", federation.CapConfigWrite)
	}
	if !federation.Check(resolved, federation.CapLLMsWrite) {
		t.Errorf("alias enabled: resolved grant must include %s", federation.CapLLMsWrite)
	}
	if !federation.Check(resolved, federation.CapUsageWrite) {
		t.Errorf("alias enabled: resolved grant must include %s", federation.CapUsageWrite)
	}
}

func TestSessionDefaultGrantCaps_AliasDisabled(t *testing.T) {
	grant := federation.SessionDefaultGrantCaps(false)
	if slices.Contains(grant, federation.SelfConfigGroup) {
		t.Errorf("disabled grant must NOT carry the %q group, got %v", federation.SelfConfigGroup, grant)
	}

	resolved := federation.Resolve(grant, nil)
	if federation.Check(resolved, federation.CapConfigWrite) {
		t.Errorf("alias disabled: resolved grant must not include %s", federation.CapConfigWrite)
	}
	// session-default must be intact in both modes.
	if !federation.Check(resolved, federation.CapSessionsRead) {
		t.Errorf("session-default capabilities must remain granted: got %v", resolved)
	}
}

func TestSelfConfigGroup_MemberAndConstantAgreement(t *testing.T) {
	if federation.SelfConfigGroup != "self-config" {
		t.Errorf("SelfConfigGroup constant drifted: %q", federation.SelfConfigGroup)
	}
	g := federation.BuiltinGroups[federation.SelfConfigGroup]
	if g == nil {
		t.Fatal("the alias group must be a builtin")
	}
	if !g.Builtin {
		t.Error("the alias group must be builtin (never operator-writable)")
	}
	if got := g.Caps; !slices.Contains(got, federation.CapConfigWrite) {
		t.Errorf("self-config group must grant config:write, got %v", got)
	}
}
