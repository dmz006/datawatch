package router

import (
	"net/http"
	"strings"
	"testing"
)

// TestTriageCommVerbIsExcluded pins the comm half of the triage read's
// parity decision: the triage read ships on REST and MCP only. The CLI and
// comm surfaces are deliberately out of this read slice — the record
// already shows on the Automaton itself in the reads an operator already
// has, and the CLI, comm and client surfaces ship together with the
// autonomous how-to pass. The reason is stated in the plan's "Parity
// surface" section (docs/plans/2026-10-26-autonomous-triage-read-route.md);
// this test pins the comm side so the exclusion cannot drift silently.
// When a slice ships the "autonomous triage" verb, this test changes with
// that section.
func TestTriageCommVerbIsExcluded(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"records":[],"alert_minutes":30}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous triage"), " ")
	if !strings.Contains(got, "usage: autonomous") {
		t.Errorf(`"autonomous triage" must still be a non-verb (excluded from the triage read slice by design); got %q`, got)
	}
	if log.calls != 0 {
		t.Errorf("the non-verb refusal must not reach the daemon, got %d calls", log.calls)
	}
}
