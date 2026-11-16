package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// holdCountsNow is the fixed reading of the clock the count tests use.
var holdCountsNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// holdCountsRecords is one running, one blocked, one needs_review and two held
// Automata.
func holdCountsRecords() []*PRD {
	mk := func(id string, status PRDStatus) *PRD {
		return &PRD{ID: id, Title: "Automaton " + id, Status: status, CreatedAt: holdCountsNow.Add(-time.Hour)}
	}
	return []*PRD{
		mk("run00001", PRDRunning),
		mk("blk00001", PRDBlocked),
		mk("rev00001", PRDNeedsReview),
		mk("hld00001", PRDHeld),
		mk("hld00002", PRDHeld),
	}
}

func TestHoldProgressCountsHeldSeparately(t *testing.T) {
	m := ComputeProgress(ProgressInput{
		PRDs:    holdCountsRecords(),
		Options: ProgressOptions{Now: holdCountsNow},
	})
	g := m.Totals
	if g.Held != 2 || g.Blocked != 1 || g.Waiting != 1 || g.Running != 1 {
		t.Fatalf("totals held=%d blocked=%d waiting=%d running=%d, want 2/1/1/1", g.Held, g.Blocked, g.Waiting, g.Running)
	}
	if g.PRDs != 5 {
		t.Fatalf("the total still includes held records: got %d, want 5", g.PRDs)
	}
	// The group merge sums held across kinds: the totals are that sum.
	sum := 0
	for _, grp := range m.Groups {
		sum += grp.Held
	}
	if sum != g.Held {
		t.Fatalf("groups hold %d held, totals %d", sum, g.Held)
	}
	if m.Grouped.Totals.Held != 2 || m.Grouped.Totals.Blocked != 1 || m.Grouped.Totals.Waiting != 1 {
		t.Fatalf("grouped totals = %+v", m.Grouped.Totals)
	}
	// Held work is still unfinished work.
	if m.Remaining.Known.PRDs != 5 {
		t.Fatalf("known unfinished Automata = %d, want 5", m.Remaining.Known.PRDs)
	}
}

func TestHoldProgressMergeSumsHeld(t *testing.T) {
	a := &progressAcc{g: ProgressGroup{PRDs: 2, Held: 1}}
	a.add(&progressAcc{g: ProgressGroup{PRDs: 3, Held: 2}})
	if a.g.Held != 3 || a.g.PRDs != 5 {
		t.Fatalf("merged held=%d prds=%d, want 3 and 5", a.g.Held, a.g.PRDs)
	}
	if got := a.finish().Held; got != 3 {
		t.Fatalf("finished held=%d, want 3", got)
	}
}

func TestHoldProgressTextPrintsHeldCount(t *testing.T) {
	withHold := RenderProgressText(ComputeProgress(ProgressInput{
		PRDs:    holdCountsRecords(),
		Options: ProgressOptions{Now: holdCountsNow},
	}))
	lines := strings.Split(withHold, "\n")
	var header, total string
	for _, l := range lines {
		if strings.Contains(l, "blocked") && strings.Contains(l, "Automata") {
			header = l
		}
		if strings.HasPrefix(l, "total") {
			total = l
		}
	}
	if !strings.Contains(header, "held") {
		t.Fatalf("header has no held column: %q", header)
	}
	fields := strings.Fields(total)
	// total, Automata, done, run, wait, blocked, held
	if len(fields) < 7 || fields[1] != "5" || fields[5] != "1" || fields[6] != "2" {
		t.Fatalf("total line %q does not carry blocked 1 and held 2", total)
	}
	if !strings.Contains(withHold, "2 held") {
		t.Fatalf("the left line does not name the held Automata:\n%s", withHold)
	}

	noHold := RenderProgressText(ComputeProgress(ProgressInput{
		PRDs:    holdCountsRecords()[:3],
		Options: ProgressOptions{Now: holdCountsNow},
	}))
	if strings.Contains(noHold, "held") {
		t.Fatalf("a table with no holds must keep its shape:\n%s", noHold)
	}
}

func TestHoldProgressSnapshotRowKeepsHeldAndOldRowReadsZero(t *testing.T) {
	m := ComputeProgress(ProgressInput{
		PRDs:    holdCountsRecords(),
		Options: ProgressOptions{Now: holdCountsNow},
	})
	snap := ProgressSnapshot{Date: "2026-11-14", Reason: "test", Metrics: m, Grouped: m.Grouped}
	tr := Trend([]ProgressSnapshot{snap}, ProjectKeyNone)
	if len(tr.ProjectPoints) != 1 || tr.ProjectPoints[0].Held != 2 {
		t.Fatalf("project points = %+v, want held 2", tr.ProjectPoints)
	}

	var old ProgressGroup
	if err := json.Unmarshal([]byte(`{"prds":4,"done":1,"running":1,"waiting":1,"blocked":1}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Held != 0 {
		t.Fatalf("an older saved group read held=%d, want 0", old.Held)
	}
	raw, err := json.Marshal(m.Totals)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"held":2`) {
		t.Fatalf("group json has no held key: %s", raw)
	}
}

func TestHoldStatusCountsHeldNotBlocked(t *testing.T) {
	m, _ := newHoldManager(t)
	// One stays blocked; one blocked and one draft are put on hold.
	stillBlocked := newOperatorHoldPRD(t, m, PRDBlocked, "judge said block")
	heldBlocked := newOperatorHoldPRD(t, m, PRDBlocked, "another block")
	heldDraft := newOperatorHoldPRD(t, m, PRDDraft, "")
	for _, id := range []string{heldBlocked.ID, heldDraft.ID} {
		if _, err := m.Hold(id, HoldRequest{Reason: "until after the release"}, "operator"); err != nil {
			t.Fatalf("Hold %s: %v", id, err)
		}
	}
	st := m.Status()
	if st.BlockedPRDs != 1 {
		t.Fatalf("BlockedPRDs = %d, want 1 (only %s)", st.BlockedPRDs, stillBlocked.ID)
	}
	if st.HeldPRDs != 2 {
		t.Fatalf("HeldPRDs = %d, want 2", st.HeldPRDs)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"held_prds":2`) {
		t.Fatalf("status json has no held_prds: %s", raw)
	}

	// Releasing a hold moves it back out of the held count.
	if _, err := m.ReleaseHold(heldBlocked.ID, "operator", ""); err != nil {
		t.Fatalf("ReleaseHold: %v", err)
	}
	st = m.Status()
	if st.HeldPRDs != 1 || st.BlockedPRDs != 2 {
		t.Fatalf("after release held=%d blocked=%d, want 1 and 2", st.HeldPRDs, st.BlockedPRDs)
	}
}

func TestHoldTriageIgnoresHeld(t *testing.T) {
	h := newTriageHarness(t, 2, 10)
	h.failPRD(t, 0, "boom-one", "operator ended it")
	h.failPRD(t, 1, "boom-two", "operator ended it")
	h.h.c.triagePass()
	if got := len(h.h.m.ListTriageRecords()); got != 2 {
		t.Fatalf("records before the hold = %d, want 2", got)
	}

	held := h.h.prds[0].ID
	if _, err := h.h.m.Hold(held, HoldRequest{Reason: "wait for the release"}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	// The read drops the held Automaton's record; the record itself stays on
	// the Automaton so a release can bring it back.
	recs := h.h.m.ListTriageRecords()
	if len(recs) != 1 || recs[0].PRDID == held {
		t.Fatalf("records after the hold = %+v, want only the other Automaton", recs)
	}
	if h.record(t, 0) == nil {
		t.Fatal("the stored record must stay on the held Automaton")
	}

	// Past the alert threshold, only the Automaton that is still stuck alerts,
	// and the held one is never raised, then or on a later pass.
	h.advance(11 * time.Minute)
	h.h.c.triagePass()
	h.h.c.triagePass()
	h.assertAlerts(t, 1, "warn|Triage:")
	entry := h.alertEntry(t, "warn|Triage:")
	if strings.Contains(entry, "boom-one") {
		t.Fatalf("a triage alert carries the held Automaton: %s", entry)
	}
	if rec := h.record(t, 0); rec == nil || rec.AlertedAt != nil {
		t.Fatalf("the held record was alerted or cleared: %+v", rec)
	}
}

func TestHoldTriageBlockedAlertTextSkipsHeldMember(t *testing.T) {
	h := newTriageHarness(t, 1, 10)
	h.failPRD(t, 0, "boom-secret", "operator ended it")
	a := h.h.makeAuto(nil, 1)
	if got := h.h.c.blockedTaskDetails(a); !strings.Contains(got, "boom-secret") {
		t.Fatalf("control: the failed member's error should be in the alert text, got %q", got)
	}
	if _, err := h.h.m.Hold(h.h.prds[0].ID, HoldRequest{Reason: "wait for the release"}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if got := h.h.c.blockedTaskDetails(a); got != "" {
		t.Fatalf("a held member must add no text to the blocked alert, got %q", got)
	}
}

func TestHoldDependencyUpdateDoesNotCountHeldAsWaiting(t *testing.T) {
	for status, want := range map[PRDStatus]bool{
		PRDBlocked:     true,
		PRDNeedsReview: true,
		PRDHeld:        false,
		PRDRunning:     false,
		PRDDraft:       false,
	} {
		if got := dependencyUpdateWaitingForOperator(status); got != want {
			t.Errorf("waiting(%s) = %v, want %v", status, got, want)
		}
	}

	// A finished run with an update stays quiet while held; the same run
	// blocked raises its alert.
	h, alerts := startedDependencyRun(t)
	finishDependencyRun(t, h, PRDHeld, []byte(`{"updated":["a"]}`))
	h.tick("2026-11-09 05:05")
	if len(*alerts) != 0 {
		t.Fatalf("a held run raised %v", *alerts)
	}
	if info := h.c.DependencyUpdateRun(); info.Finished || info.Status != string(PRDHeld) {
		t.Fatalf("a held run reports finished=%v status=%q, want not finished and held", info.Finished, info.Status)
	}

	h2, alerts2 := startedDependencyRun(t)
	finishDependencyRun(t, h2, PRDBlocked, []byte(`{"updated":["a"]}`))
	h2.tick("2026-11-09 05:05")
	if len(*alerts2) != 1 {
		t.Fatalf("a blocked run should alert once, got %v", *alerts2)
	}
}
