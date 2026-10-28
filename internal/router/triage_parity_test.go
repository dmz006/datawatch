package router

import (
	"slices"
	"strings"
	"testing"
)

// TestTriageCommVerbIsRegistered pins the comm half of the triage read's
// parity decision: "autonomous triage" is a registered comm verb. The
// plan (docs/plans/2026-10-26-autonomous-triage-read-route.md, "Parity
// surface") ships the read on all four surfaces — the REST route, the
// autonomous_triage MCP tool, the datawatch autonomous triage CLI
// subcommand, and this comm verb — and this test pins the comm side so
// it cannot drift back to a non-verb. The rendering contract is in
// triage_comm_test.go.
func TestTriageCommVerbIsRegistered(t *testing.T) {
	if !slices.Contains(TriageCommVerbs(), "triage") {
		t.Fatalf("TriageCommVerbs() = %v, must contain %q", TriageCommVerbs(), "triage")
	}
	verbs := TriageCommVerbs()
	for _, verb := range verbs {
		c := Parse("autonomous " + verb)
		if c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}

	// The autonomous command's own help must advertise the verb.
	r := setupPeerRouter(t, 0)
	got := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(got, "triage [automation-id]") {
		t.Errorf("the autonomous help must list the triage verb; got:\n%s", got)
	}
}
