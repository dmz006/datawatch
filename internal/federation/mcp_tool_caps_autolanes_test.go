package federation_test

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

// Auto-lanes is the usage calculator's loop and its two settings
// (docs/plans/2026-10-27-auto-lane-loops-design.md). The read-only status
// tool maps to the calculator's own read capability — the same cap the
// burn-guard status reads use — so a client holding usage:read can see where
// the loop stands; the tool writes nothing, so no config capability is
// implied.
func TestMCPToolCap_UsageAutoLanesStatusIsUsageRead(t *testing.T) {
	cap, ok := federation.MCPToolCap["usage_autolanes_status"]
	if !ok {
		t.Fatal("usage_autolanes_status is missing from MCPToolCap — it would be callable by nobody but admin")
	}
	want := federation.CapUsageRead
	if cap != want {
		t.Errorf("usage_autolanes_status cap = %q, want %q", cap, want)
	}

	// A client holding usage:read can therefore call it.
	if !federation.Check([]string{federation.CapUsageRead}, cap) {
		t.Errorf("usage:read does not cover usage_autolanes_status (cap %q)", cap)
	}
	// …and a client without it cannot.
	if federation.Check([]string{federation.CapConfigRead}, cap) {
		t.Errorf("config:read alone covers usage_autolanes_status, want only usage:read")
	}
}
