package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func countRevList(g *aumGit, branch string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		if c[0] == "rev-list" && strings.HasSuffix(c[len(c)-1], ".."+branch) {
			n++
		}
	}
	return n
}

// The archive step: a record the close step keeps is reported once and its
// branch is not compared again on every pass while the record is unchanged.
func TestCloseStepKeptRecordIsReportedOnceAndNotRetriedEveryPass(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "3\n"}}
	h, steps, at := aumRig(t, git, 1)
	id := aumArchive(t, h, 0, "work/a", "Alpha", at)
	p, _ := h.m.Store().GetPRD(id)
	p.Status, p.Archive = PRDCompleted, nil
	h.m.Store().WithLock(func() { p.UpdatedAt = at.Add(-time.Hour) })

	for i := 0; i < 4; i++ {
		r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
		if r.Err != nil || !strings.Contains(r.Detail, "kept 1 completed") {
			t.Fatalf("pass %d: %+v", i, r)
		}
	}
	if n := countRevList(git, "work/a"); n != 1 {
		t.Errorf("branch compared %d times over 4 passes, want 1 (the refusal is held)", n)
	}
	var e *RefusalEntry
	for _, x := range h.m.RefusalLedger().List() {
		if x.Automaton == id && x.Step == "archive" {
			x := x
			e = &x
		}
	}
	if e == nil || e.Repeats != 1 || !strings.Contains(e.Reason, "3 ahead") {
		t.Fatalf("ledger entry = %+v, want one entry naming the 3 commits, repeated once", e)
	}
	if got := steps.HeldReason(id); !strings.Contains(got, "3 ahead") {
		t.Errorf("held reason %q must still show while the refusal is held", got)
	}

	// A change to the record lets the step try again.
	h.m.Store().WithLock(func() { p.UpdatedAt = at.Add(time.Minute) })
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if n := countRevList(git, "work/a"); n != 2 {
		t.Errorf("after the record changed: %d comparisons, want 2", n)
	}
	// So does the hold running out.
	steps.now = func() time.Time { return at.Add(RefusalRetryHold + time.Hour) }
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if n := countRevList(git, "work/a"); n != 3 {
		t.Errorf("after the hold ran out: %d comparisons, want 3", n)
	}
}

// Once the work lands the step goes through and the refusal is forgotten.
func TestCloseStepForgetsTheRefusalWhenTheWorkLands(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "3\n"}}
	h, steps, at := aumRig(t, git, 1)
	id := aumArchive(t, h, 0, "work/a", "Alpha", at)
	p, _ := h.m.Store().GetPRD(id)
	p.Status, p.Archive = PRDCompleted, nil
	h.m.Store().WithLock(func() { p.UpdatedAt = at.Add(-time.Hour) })
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	git.ahead["work/a"] = "0\n"
	steps.now = func() time.Time { return at.Add(RefusalRetryHold + time.Hour) }
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if st := h.m.Store(); func() PRDStatus { q, _ := st.GetPRD(id); return q.Status }() != PRDArchived {
		t.Error("the record should archive once its branch is on main")
	}
	for _, x := range h.m.RefusalLedger().List() {
		if x.Automaton == id && x.Step == "archive" {
			t.Errorf("the refusal should be forgotten after the step went through: %+v", x)
		}
	}
}

// The repair step: a repair that failed is reported once, not retried each pass.
func TestRepairThatFailedIsReportedOnceAndNotRetriedEveryPass(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "task x/t1", Summary: "stuck"}}, err: context.DeadlineExceeded}
	h := newHarness(t, "on", f)
	ledger := NewRefusalLedger(t.TempDir())
	h.r.hooks.Ledger = ledger
	for i := 0; i < 5; i++ {
		h.r.Run(context.Background())
		h.now = h.now.Add(time.Minute)
	}
	if len(f.applied) != 1 {
		t.Errorf("repair applied %d times over 5 passes, want 1", len(f.applied))
	}
	if n := len(h.r.Records()); n != 1 {
		t.Errorf("records = %d, want the failure reported once", n)
	}
	l := ledger.List()
	if len(l) != 1 || l[0].Step != "repair:task_chain" || l[0].Repeats != 1 {
		t.Errorf("ledger = %+v", l)
	}
	h.now = h.now.Add(RefusalRetryHold + time.Minute)
	h.r.Run(context.Background())
	if len(f.applied) != 2 {
		t.Errorf("after the hold ran out the repair should be tried again, applied %d", len(f.applied))
	}
}

func TestRepairerWithoutALedgerKeepsRetrying(t *testing.T) {
	f := &fakeRepair{group: RepairGroupTaskChain, findings: []RepairFinding{{Subject: "t", Summary: "stuck"}}, err: context.DeadlineExceeded}
	h := newHarness(t, "on", f)
	for i := 0; i < 3; i++ {
		h.r.Run(context.Background())
	}
	if len(f.applied) != 3 {
		t.Errorf("applied %d, want 3 without a ledger (unchanged behaviour)", len(f.applied))
	}
}

// The merge step: a comparison that cannot be made is refused once and not
// retried on every pass.
func TestMergeStepUnknownComparisonIsRefusedOnceAndHeld(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	git.tipsUnreadable = true
	git.failOnVerb = "rev-parse"
	// The record was last changed before the harness clock's first pass.
	rec, _ := th.h.m.Store().GetPRD(th.h.prds[0].ID)
	th.h.m.Store().WithLock(func() { rec.UpdatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) })
	for i := 0; i < 4; i++ {
		th.h.c.triagePass()
	}
	if n := git.count("rev-parse"); n > 2 {
		t.Errorf("rev-parse ran %d times over 4 passes with no time passing, want it held after the first refusal", n)
	}
	var e *RefusalEntry
	for _, x := range th.h.m.RefusalLedger().List() {
		if x.Step == "merge" {
			x := x
			e = &x
		}
	}
	if e == nil || e.Repeats != 1 {
		t.Fatalf("ledger entry = %+v, want one merge refusal", e)
	}
}
