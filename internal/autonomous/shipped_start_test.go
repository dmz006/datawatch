// Tests for the daemon-start shipped backfill (autonomous.shipped
// backfill_on_start): enabled, one pass runs and marks; disabled, nothing
// runs; StartShippedBackfill never blocks the caller. The shared fixture
// from shipped_backfill_test.go supplies the throwaway repo, store and the
// pre-built PRDs/Automations; HOME is a temp dir, so nothing here touches
// the operator's home, daemon or sessions.

package autonomous

import (
	"testing"
	"time"
)

func TestShippedStartBackfillDisabledDoesNotRun(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")

	fx.m.StartShippedBackfill(false)
	// Give a (incorrectly) started goroutine a chance to mark — it must not.
	time.Sleep(100 * time.Millisecond)

	if fx.released.IsShipped() || fx.notes.IsShipped() || fx.unreleased.IsShipped() {
		t.Errorf("disabled startup backfill marked PRDs: released=%v notes=%v unreleased=%v",
			fx.released.IsShipped(), fx.notes.IsShipped(), fx.unreleased.IsShipped())
	}
	if getStoredAutomation(t, fx.m, fx.autoOK.ID).IsShipped() {
		t.Errorf("disabled startup backfill marked the Automation")
	}
}

func TestShippedStartBackfillEnabledMarksWithoutBlocking(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")

	// The daemon calls this inline during startup: if it did not return
	// promptly, startup would hang.
	done := make(chan struct{})
	go func() {
		fx.m.StartShippedBackfill(true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StartShippedBackfill(true) blocked the caller")
	}

	// The pass runs in a goroutine and marks the merged PRDs (and the
	// all-merged Automation) exactly as a synchronous pass would. The
	// store owns the record it saved, so read the stored copies back.
	// The backfill goroutine writes the stored PRD while this test polls, so
	// read it through SnapshotPRD (a copy taken under the store lock), never
	// through the fixture's live pointer.
	snapshot := func(id string) *PRD {
		p, ok := fx.m.store.SnapshotPRD(id)
		if !ok {
			t.Fatalf("store lost prd %s", id)
		}
		return p
	}
	deadline := time.Now().Add(10 * time.Second)
	for !snapshot(fx.released.ID).IsShipped() || !getStoredAutomation(t, fx.m, fx.autoOK.ID).IsShipped() {
		if time.Now().After(deadline) {
			stored := getStoredAutomation(t, fx.m, fx.autoOK.ID)
			t.Fatalf("enabled startup backfill did not finish marking: released=%v autoOK=%v",
				snapshot(fx.released.ID).IsShipped(), stored.IsShipped())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rel := snapshot(fx.released.ID); rel.MergedInRelease != "v9.8.0" || rel.ShippedSource != ShippedSourceBackfill {
		t.Errorf("released: release=%q source=%q, want v9.8.0 backfill",
			rel.MergedInRelease, rel.ShippedSource)
	}
	if ok := getStoredAutomation(t, fx.m, fx.autoOK.ID); !ok.IsShipped() || ok.MergedInRelease != "v9.9.0" {
		t.Errorf("autoOK: shipped=%v release=%q, want shipped v9.9.0", ok.IsShipped(), ok.MergedInRelease)
	}
	// Statuses are never rewritten by startup either.
	if fx.cancelled.Status != PRDCancelled || fx.cancelled.IsShipped() {
		t.Errorf("cancelled PRD changed by startup: status=%s shipped=%v",
			fx.cancelled.Status, fx.cancelled.IsShipped())
	}
}

func TestShippedStartBackfillOnceWrapsDryRun(t *testing.T) {
	fx := newShippedBackfillFixture(t, "main")

	// The synchronous entry point every operator surface goes through.
	rep, err := fx.m.ShippedBackfillOnce(true)
	if err != nil {
		t.Fatalf("ShippedBackfillOnce(dry): %v", err)
	}
	dry, ok := rep.(ShippedBackfillReport)
	if !ok {
		t.Fatalf("ShippedBackfillOnce = %T, want ShippedBackfillReport", rep)
	}
	// The dry run reports what the writing pass would mark for PRDs (3 here)
	// and writes nothing; Automations are only counted when written.
	if dry.PrdsMarked != 3 || dry.AutomationsMarked != 0 {
		t.Errorf("dry run counted %d prds, %d automations, want 3/0", dry.PrdsMarked, dry.AutomationsMarked)
	}
	if dry.PrdsScanned != 4 || len(dry.UnmatchedIDs) != 1 || dry.UnmatchedIDs[0] != "deadbeef" {
		t.Errorf("dry run report = %+v, want 4 scanned, [deadbeef] unmatched", dry)
	}

	// The writing pass marks exactly as the direct call does.
	rep2, err := fx.m.ShippedBackfillOnce(false)
	if err != nil {
		t.Fatalf("ShippedBackfillOnce(run): %v", err)
	}
	run, ok := rep2.(ShippedBackfillReport)
	if !ok {
		t.Fatalf("ShippedBackfillOnce = %T, want ShippedBackfillReport", rep2)
	}
	if run.PrdsMarked != 3 || run.AutomationsMarked != 1 {
		t.Errorf("run marked %d prds, %d automations, want 3/1: %+v", run.PrdsMarked, run.AutomationsMarked, run)
	}
}
