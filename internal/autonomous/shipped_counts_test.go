// Shipped-state counts, effective display status and shipped selection.
//
// The one scenario everything hangs off: a PRD cancelled without its work
// landing stays cancelled and unshipped; a PRD whose work landed stays
// cancelled in the store AND shipped, and the shipped-state surfaces treat
// the two differently: with the default settings the counts separate them
// (shipped apart from cancelled) and the effective display status of the
// shipped one is "shipped"; with count_separately=false the shipped one
// counts under its stored status (cancelled) instead.

package autonomous

import (
	"testing"
)

func shippedCountFixture(t *testing.T) []*PRD {
	t.Helper()
	return []*PRD{
		{ID: "p-cancelled", Status: PRDCancelled},
		{ID: "p-shipped-cancelled", Status: PRDCancelled, MergeCommit: "c0ffee42"},
		{ID: "p-shipped-completed", Status: PRDCompleted, MergeCommit: "bead1"},
		{ID: "p-running", Status: PRDRunning},
	}
}

func TestShippedCounts_DefaultSeparateCancelledAndShipped(t *testing.T) {
	// Default settings: enabled + count_separately, so the two cancelled
	// PRDs split — the shipped one lands in "shipped", the plain one in
	// "cancelled".
	counts := CountPRDStates(shippedCountFixture(t), true, true)
	if counts["shipped"] != 2 {
		t.Errorf("shipped bucket = %d, want 2 (both shipped PRDs)", counts["shipped"])
	}
	if counts["cancelled"] != 1 {
		t.Errorf("cancelled bucket = %d, want 1 (only the unshipped cancelled PRD)", counts["cancelled"])
	}
	if counts["completed"] != 0 || counts["running"] != 1 {
		t.Errorf("other buckets = %v; want completed absent, running 1", counts)
	}
}

func TestShippedCounts_CountSeparatelyOffUsesStoredStatus(t *testing.T) {
	// count_separately=false folds the shipped PRDs back into their stored
	// statuses: the shipped-but-cancelled PRD counts as cancelled.
	counts := CountPRDStates(shippedCountFixture(t), true, false)
	if counts["cancelled"] != 2 {
		t.Errorf("cancelled bucket = %d, want 2 (the shipped one folds back in)", counts["cancelled"])
	}
	if c, ok := counts["shipped"]; ok && c != 0 {
		t.Errorf("shipped bucket = %d, want absent (counts under stored status)", c)
	}
	if counts["running"] != 1 || counts["completed"] != 1 {
		t.Errorf("stored-status buckets = %v; want completed 1, running 1", counts)
	}
}

func TestShippedCounts_SurfaceOffUsesStoredStatus(t *testing.T) {
	// Shipped surface disabled: the enabled flag alone is not enough — the
	// shipped PRDs still fold back into their stored statuses.
	counts := CountPRDStates(shippedCountFixture(t), false, true)
	if counts["cancelled"] != 2 {
		t.Errorf("cancelled bucket = %d, want 2 (surface off: no shipped bucket)", counts["cancelled"])
	}
	if c, ok := counts["shipped"]; ok && c != 0 {
		t.Errorf("shipped bucket = %d, want absent", c)
	}
}

func TestShippedCounts_AutomationsSameSeparation(t *testing.T) {
	autos := []*Automation{
		{ID: "a-plain-cancelled", Status: AutomationCancelled},
		{ID: "a-shipped-cancelled", Status: AutomationCancelled, MergeCommit: "c0ffee42"},
		{ID: "a-active", Status: AutomationActive},
	}
	counts := CountAutomationStates(autos, true, true)
	if counts["shipped"] != 1 || counts["cancelled"] != 1 || counts["active"] != 1 {
		t.Fatalf("buckets = %v; want shipped 1, cancelled 1, active 1", counts)
	}
	if folded := CountAutomationStates(autos, true, false); folded["cancelled"] != 2 {
		t.Errorf("count_separately off: cancelled = %d, want 2", folded["cancelled"])
	}
}

func TestShippedEffectiveDisplayStatus(t *testing.T) {
	// The cancelled-but-shipped PRD displays as shipped — its stored status
	// never moves.
	if got := PRDEffectiveDisplayStatus(shippedCountFixture(t)[1], true); got != "shipped" {
		t.Errorf("effective status = %q, want shipped", got)
	}
	// ...while both settings-off paths and the unshipped PRD show the stored
	// status.
	if got := PRDEffectiveDisplayStatus(shippedCountFixture(t)[1], false); got != "cancelled" {
		t.Errorf("surface off: effective status = %q, want cancelled (stored)", got)
	}
	if got := PRDEffectiveDisplayStatus(shippedCountFixture(t)[0], true); got != "cancelled" {
		t.Errorf("unshipped PRD: effective status = %q, want cancelled", got)
	}
	if got := PRDEffectiveDisplayStatus(nil, true); got != "" {
		t.Errorf("nil PRD: effective status = %q, want empty", got)
	}
	a := &Automation{ID: "a1", Status: AutomationCancelled, MergeCommit: "c1"}
	if got := AutomationEffectiveDisplayStatus(a, true); got != "shipped" {
		t.Errorf("automation effective status = %q, want shipped", got)
	}
	if got := AutomationEffectiveDisplayStatus(a, false); got != "cancelled" {
		t.Errorf("automation, surface off: effective status = %q, want cancelled", got)
	}
}

func TestShippedSelection_FiltersSplitShippedFromNot(t *testing.T) {
	fix := shippedCountFixture(t)
	var keptShipped, keptUnshipped []*PRD
	for _, p := range fix {
		if PRDShippedMatch(p, true, true) {
			keptShipped = append(keptShipped, p)
		}
		if PRDShippedMatch(p, true, false) {
			keptUnshipped = append(keptUnshipped, p)
		}
	}
	if len(keptShipped) != 2 || len(keptUnshipped) != 2 {
		t.Fatalf("shipped kept %v, unshipped kept %v; want 2/2",
			idsOfPRDs(keptShipped), idsOfPRDs(keptUnshipped))
	}
	for _, p := range keptShipped {
		if !p.IsShipped() {
			t.Errorf("shipped selection kept an unshipped PRD: %q", p.ID)
		}
	}
	// The surface-off selection keeps everything — there is nothing to
	// select on.
	kept := 0
	for _, p := range fix {
		if PRDShippedMatch(p, false, true) {
			kept++
		}
	}
	if kept != len(fix) {
		t.Errorf("surface off: kept %d, want all %d", kept, len(fix))
	}
}

func idsOfPRDs(prds []*PRD) []string {
	out := make([]string, 0, len(prds))
	for _, p := range prds {
		out = append(out, p.ID)
	}
	return out
}

func TestShippedManagerCounts_NilManagerEmpty(t *testing.T) {
	var m *Manager
	counts := m.ShippedCounts(true, true)
	if counts.PRDs == nil || counts.Automations == nil {
		t.Fatalf("nil manager: want non-nil empty buckets, got %+v", counts)
	}
	if len(counts.PRDs) != 0 || len(counts.Automations) != 0 {
		t.Errorf("nil manager: want empty counts, got %+v", counts)
	}
}
