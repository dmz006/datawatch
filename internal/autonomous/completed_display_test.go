// Completed display: a record carrying a shipped record shows and counts as
// Completed, and the release detail is returned only for merged code.

package autonomous

import (
	"testing"
	"time"
)

func TestCompletedDisplayStatusForShippedCancelledRecord(t *testing.T) {
	p := &PRD{ID: "p1", Status: PRDCancelled, MergeCommit: "c0ffee"}
	if got := PRDEffectiveDisplayStatus(p, true); got != "completed" {
		t.Errorf("PRD display = %q, want completed", got)
	}
	a := &Automation{ID: "a1", Status: AutomationCancelled, MergeCommit: "c0ffee"}
	if got := AutomationEffectiveDisplayStatus(a, true); got != "completed" {
		t.Errorf("Automation display = %q, want completed", got)
	}
	if got := PRDEffectiveDisplayStatus(&PRD{Status: PRDCancelled}, true); got != "cancelled" {
		t.Errorf("unshipped cancelled display = %q, want cancelled", got)
	}
	if got := PRDEffectiveDisplayStatus(p, false); got != "cancelled" {
		t.Errorf("surface off display = %q, want stored cancelled", got)
	}
	if CompletedDisplayStatus != "completed" {
		t.Errorf("CompletedDisplayStatus = %q", CompletedDisplayStatus)
	}
}

func TestCompletedDisplayCountBuckets(t *testing.T) {
	prds := []*PRD{
		{ID: "stored-completed", Status: PRDCompleted},
		{ID: "cancelled-merged", Status: PRDCancelled, MergeCommit: "abc"},
		{ID: "cancelled-plain", Status: PRDCancelled},
	}
	for _, sep := range []bool{true, false} {
		got := CountPRDStates(prds, true, sep)
		if got["completed"] != 2 {
			t.Errorf("sep=%v completed = %d, want 2 (%v)", sep, got["completed"], got)
		}
		if got["cancelled"] != 1 {
			t.Errorf("sep=%v cancelled = %d, want 1 (%v)", sep, got["cancelled"], got)
		}
		if _, ok := got["shipped"]; ok {
			t.Errorf("sep=%v shipped bucket still present: %v", sep, got)
		}
	}
	autos := []*Automation{
		{ID: "a-done", Status: AutomationCompleted},
		{ID: "a-merged", Status: AutomationCancelled, MergeCommit: "abc"},
		{ID: "a-plain", Status: AutomationCancelled},
	}
	bc := BuildShippedCounts(prds, autos, true, true)
	if bc.Automations["completed"] != 2 || bc.Automations["cancelled"] != 1 || bc.PRDs["completed"] != 2 {
		t.Errorf("BuildShippedCounts = %+v", bc)
	}
	if _, ok := bc.Automations["shipped"]; ok {
		t.Errorf("automation shipped bucket present: %v", bc.Automations)
	}
}

func TestCompletedDisplayDetailFieldsOnlyWithMergeCommit(t *testing.T) {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	merged := &PRD{ID: "m", Status: PRDCancelled, MergeCommit: "abc", MergedInRelease: "v9.1.0", MergedAt: &at, ShippedSource: ShippedSourceBackfill}
	v := BuildSummaryView(merged)
	if v.EffectiveStatus != "completed" || v.MergedInRelease != "v9.1.0" || v.MergeCommit != "abc" || v.MergedAt == nil {
		t.Errorf("merged view = %+v", v)
	}
	plain := &PRD{ID: "n", Status: PRDCompleted, MergedInRelease: "v9.1.0", MergedAt: &at}
	pv := BuildSummaryView(plain)
	if pv.MergedInRelease != "" || pv.MergedAt != nil || pv.MergeCommit != "" || pv.ShippedSource != "" {
		t.Errorf("record without merge commit leaked release detail: %+v", pv)
	}
	if pv.EffectiveStatus != "completed" {
		t.Errorf("stored-completed display = %q", pv.EffectiveStatus)
	}
}
