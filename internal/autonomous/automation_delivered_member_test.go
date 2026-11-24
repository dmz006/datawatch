package autonomous

import (
	"strings"
	"testing"
	"time"
)

// deliveredHarness is a linear chain of n members, all approved, entered at m0
// the way the coordinator-test harness does it. Members listed in shipped carry
// their own shipped record.
func deliveredHarness(t *testing.T, n int, shipped ...int) (*coordHarness, *Automation) {
	t.Helper()
	h := newCoordHarness(t, n)
	var edges [][3]string
	for i := 0; i < n-1; i++ {
		edges = append(edges, [3]string{"m" + string(rune('0'+i+1)), "", ""})
	}
	a := h.force(h.makeAuto(edges, n))
	for _, i := range shipped {
		h.markShipped(i)
	}
	return h, a
}

func (h *coordHarness) markShipped(i int) {
	h.t.Helper()
	p, ok := h.m.store.GetPRD(h.prds[i].ID)
	if !ok {
		h.t.Fatalf("prd %d missing", i)
	}
	p.MergedInRelease, p.MergeCommit = shippedRelease, shippedCommit
	if err := h.m.store.SavePRD(p); err != nil {
		h.t.Fatal(err)
	}
}

func (h *coordHarness) alertText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.alerts, "\n")
}

func (h *coordHarness) ranIDs() []string { r, _ := h.fake.counts(); return r }

// The next member is already delivered: the Automation skips it, records why
// and starts the member after it. Nothing blocks, and the delivered member is
// never started.
func TestAutomationSkipsDeliveredNextMember(t *testing.T) {
	h, a := deliveredHarness(t, 3, 1)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	if runs := h.ranIDs(); len(runs) != 1 || runs[0] != h.prds[2].ID {
		t.Fatalf("starter runs = %v, want only the third member %s (the delivered one must never start)", runs, h.prds[2].ID)
	}
	got := h.get(a)
	if got.Status != AutomationActive || got.CurrentMemberID != "m2" {
		t.Fatalf("status=%s cursor=%q, want active at m2", got.Status, got.CurrentMemberID)
	}
	m1 := got.Members[1]
	if m1.Status != MemberCompleted || m1.StartedAt != nil || m1.Outcome != "already_shipped" || m1.EdgeTaken != "on_complete" {
		t.Fatalf("delivered member = %+v, want completed without a start, outcome already_shipped, edge on_complete", m1)
	}
	var found *Decision
	for i := range got.Decisions {
		if got.Decisions[i].Kind == DecisionMemberDelivered {
			found = &got.Decisions[i]
		}
	}
	if found == nil || !strings.Contains(found.Note, shippedRelease) || !strings.Contains(found.Note, "bravo") {
		t.Fatalf("a decision must say which member was skipped and the release that carries it: %+v", found)
	}
	if !h.hasAudit("automation_member_delivered") {
		t.Fatalf("no audit entry for the skip: %v", h.audits)
	}
	if hasCoordDecision(got.Decisions, "blocked") || h.alertCount("warn|Automation blocked") != 0 {
		t.Fatalf("a delivered member must not block the Automation: %+v alerts=%v", got.Decisions, h.alerts)
	}
	p1, _ := h.m.store.GetPRD(h.prds[1].ID)
	if p1.Status != PRDCompleted || p1.ShippedRefusal == nil || p1.ShippedRefusal.Release != shippedRelease {
		t.Fatalf("the delivered member's own record must say it is delivered with the reason: status=%s refusal=%+v", p1.Status, p1.ShippedRefusal)
	}
}

func TestAutomationSkipsSeveralDeliveredMembersInARow(t *testing.T) {
	h, a := deliveredHarness(t, 4, 1, 2)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	if runs := h.ranIDs(); len(runs) != 1 || runs[0] != h.prds[3].ID {
		t.Fatalf("starter runs = %v, want only the last member", runs)
	}
	got := h.get(a)
	if got.CurrentMemberID != "m3" || got.Members[1].Outcome != "already_shipped" || got.Members[2].Outcome != "already_shipped" {
		t.Fatalf("both delivered members must be skipped and the cursor must reach m3: %+v", got.Members)
	}
}

// A delivered member at the end of the chain still lets the Automation finish.
func TestAutomationCompletesWhenTheLastMemberIsDelivered(t *testing.T) {
	h, a := deliveredHarness(t, 2, 1)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	if runs := h.ranIDs(); len(runs) != 0 {
		t.Fatalf("nothing must start, got %v", runs)
	}
	if got := h.get(a); got.Status != AutomationCompleted || got.Members[1].Outcome != "already_shipped" {
		t.Fatalf("status=%s member=%+v, want completed with the last member skipped", got.Status, got.Members[1])
	}
}

// A member that failed or blocked and was merged by hand (shipped record, status
// failed) is skipped too; it used to block the Automation.
func TestAutomationSkipsDeliveredMemberThatFailedBeforeItWasMerged(t *testing.T) {
	for _, st := range []PRDStatus{PRDFailed, PRDBlocked, PRDDraft, PRDNeedsReview} {
		h, a := deliveredHarness(t, 3, 1)
		h.setStatus(1, st)
		h.setStatus(0, PRDCompleted)
		h.event(0, PRDCompleted)
		got := h.get(a)
		if got.Status != AutomationActive || got.CurrentMemberID != "m2" || got.Members[1].Outcome != "already_shipped" {
			t.Fatalf("status %s: Automation=%s cursor=%q member=%s, want it skipped and the chain moved on", st, got.Status, got.CurrentMemberID, got.Members[1].Status)
		}
	}
}

// The entry member goes through the same step, so activation skips a delivered
// entry member too.
func TestAutomationActivationSkipsDeliveredEntryMember(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.makeAuto([][3]string{{"m1", "", ""}}, 2)
	h.markShipped(0)
	got, err := h.c.activate(a.ID, "operator", 0)
	if err != nil {
		t.Fatal(err)
	}
	if runs := h.ranIDs(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("starter runs = %v, want only the second member", runs)
	}
	if got.Status != AutomationActive || got.Members[0].Outcome != "already_shipped" || got.CurrentMemberID != "m1" {
		t.Fatalf("activation must skip the delivered entry member: %+v", got)
	}
}

// Another member builds on the delivered member's branch, so its output is
// needed: nothing is skipped automatically. The Automation blocks with a clear
// message naming both members, and the delivered member is neither started nor
// marked delivered.
func TestAutomationDoesNotSkipDeliveredMemberWhenAnotherMemberBuildsOnIt(t *testing.T) {
	h, a := deliveredHarness(t, 3, 1)
	p1, _ := h.m.store.GetPRD(h.prds[1].ID)
	p1.Git.Branch = "automaton/bravo"
	_ = h.m.store.SavePRD(p1)
	p2, _ := h.m.store.GetPRD(h.prds[2].ID)
	p2.Git.BaseBranch = "automaton/bravo"
	_ = h.m.store.SavePRD(p2)

	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	if runs := h.ranIDs(); len(runs) != 0 {
		t.Fatalf("nothing must start, got %v", runs)
	}
	got := h.get(a)
	if got.Status != AutomationBlocked || got.Members[1].Outcome == "already_shipped" {
		t.Fatalf("status=%s member=%+v, want blocked and not delivered", got.Status, got.Members[1])
	}
	last := got.Decisions[len(got.Decisions)-1]
	for _, want := range []string{"bravo", "charlie", shippedRelease, "automaton/bravo", "builds on"} {
		if !strings.Contains(h.alertText(), want) && !strings.Contains(last.Note, want) {
			t.Fatalf("the block message must mention %q: decision=%q alerts=%v", want, last.Note, h.alerts)
		}
	}
	after, _ := h.m.store.GetPRD(h.prds[1].ID)
	if after.Status == PRDCompleted || after.ShippedRefusal != nil {
		t.Fatalf("a member that is not skipped must not be marked delivered: %s %+v", after.Status, after.ShippedRefusal)
	}
}

// A dependent that already finished does not need the output any more, and a
// base branch that names some other member's branch is not a dependency.
func TestAutomationSkipsDeliveredMemberWhenNobodyPendingBuildsOnIt(t *testing.T) {
	h, a := deliveredHarness(t, 3, 1)
	p1, _ := h.m.store.GetPRD(h.prds[1].ID)
	p1.Git.Branch = "automaton/bravo"
	_ = h.m.store.SavePRD(p1)
	p2, _ := h.m.store.GetPRD(h.prds[2].ID)
	p2.Git.BaseBranch = "automaton/someone-else"
	_ = h.m.store.SavePRD(p2)

	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	if got := h.get(a); got.Status != AutomationActive || got.Members[1].Outcome != "already_shipped" {
		t.Fatalf("unrelated base branch must not stop the skip: status=%s member=%+v", got.Status, got.Members[1])
	}
}

// The setting is respected: with the rule off or in shadow mode the member is
// started as before; an operator force that covers the record starts it too.
func TestAutomationStartsShippedMemberWhenRuleIsNotOn(t *testing.T) {
	for _, mode := range []string{ShippedRefusalOff, ShippedRefusalShadow} {
		h, a := deliveredHarness(t, 3, 1)
		h.m.mu.Lock()
		h.m.cfg.ShippedRefusal = mode
		h.m.mu.Unlock()
		h.setStatus(0, PRDCompleted)
		h.event(0, PRDCompleted)
		if runs := h.ranIDs(); len(runs) != 1 || runs[0] != h.prds[1].ID {
			t.Fatalf("mode %s: starter runs = %v, want the shipped member to start as before", mode, runs)
		}
		if got := h.get(a); got.Members[1].Outcome == "already_shipped" {
			t.Fatalf("mode %s: member must not be skipped", mode)
		}
	}
	h, _ := deliveredHarness(t, 3, 1)
	p1, _ := h.m.store.GetPRD(h.prds[1].ID)
	p1.ClosedWorkOverride = &ClosedWorkOverride{Commit: shippedCommit, Actor: "operator", Reason: "redo"}
	_ = h.m.store.SavePRD(p1)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	if runs := h.ranIDs(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("a forced member must start, got %v", runs)
	}
}

// A fork branch whose own record says its work shipped counts as a finished
// branch: the join still waits for the other branch, then goes on.
func TestAutomationForkBranchThatIsDeliveredCountsAsDone(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, "p1"), st))
	h.markShipped(1) // branch b1
	h.startFork(a)

	if got := st.started(); len(got) != 1 || got[0] != h.prdID(2) {
		t.Fatalf("starts = %v, want only branch b2 (b1 is delivered)", got)
	}
	if b1 := h.member(a, "b1"); b1.Status != MemberCompleted || b1.Outcome != "already_shipped" {
		t.Fatalf("branch b1 = %+v, want completed as already_shipped", b1)
	}
	if got := h.get(a); got.Status != AutomationActive {
		t.Fatalf("status = %s, want active while b2 runs", got.Status)
	}
	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted)
	if got := st.started(); len(got) != 2 || got[1] != h.prdID(3) {
		t.Fatalf("starts = %v, want the member after the join (p1) once b2 completes", got)
	}
}

// A chain member of a fork branch that is delivered moves its own chain on.
func TestAutomationForkBranchAllDeliveredFiresTheJoin(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, "p1"), st))
	h.markShipped(1)
	h.markShipped(2)
	h.startFork(a)
	if got := st.started(); len(got) != 1 || got[0] != h.prdID(3) {
		t.Fatalf("starts = %v, want only p1 after the join when both branches are delivered", got)
	}
	if got := h.get(a); got.Status != AutomationActive {
		t.Fatalf("status = %s, want active at p1", got.Status)
	}
}

// A boot or resume pass that finds the cursor on a member that never started
// and whose own record says it shipped skips it the same way.
func TestAutomationReconcileSkipsDeliveredPendingCursor(t *testing.T) {
	h, rs := newReconcileHarness(t, 3)
	a := h.makeAuto([][3]string{{"m1", "", ""}, {"m2", "", ""}}, 3)
	h.markShipped(0)
	a, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		a.CurrentMemberID = a.EntryMemberID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.c.reconcile()

	if runs, _ := rs.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs = %v, want only the second member", runs)
	}
	got := h.get(a)
	if got.Status != AutomationActive || got.Members[0].Outcome != "already_shipped" || got.CurrentMemberID != "m1" {
		t.Fatalf("status=%s cursor=%q member=%+v, want the delivered entry skipped and m1 running", got.Status, got.CurrentMemberID, got.Members[0])
	}
}

func TestAutomationReconcileBlocksOnDeliveredPendingCursorThatAnotherMemberBuildsOn(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.makeAuto([][3]string{{"m1", "", ""}}, 2)
	h.markShipped(0)
	p0, _ := h.m.store.GetPRD(h.prds[0].ID)
	p0.Git.Branch = "automaton/alpha"
	_ = h.m.store.SavePRD(p0)
	p1, _ := h.m.store.GetPRD(h.prds[1].ID)
	p1.Git.BaseBranch = "automaton/alpha"
	_ = h.m.store.SavePRD(p1)
	a, _ = h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		a.CurrentMemberID = a.EntryMemberID
		return nil
	})
	h.c.reconcile()

	if runs, _ := rs.counts(); len(runs) != 0 {
		t.Fatalf("nothing must start, got %v", runs)
	}
	got := h.get(a)
	last := got.Decisions[len(got.Decisions)-1]
	if got.Status != AutomationBlocked || !strings.Contains(last.Note, "builds on its branch automaton/alpha") {
		t.Fatalf("status=%s last decision=%q, want blocked with the clear message", got.Status, last.Note)
	}
}

// A delivered member costs nothing, so a budget gate that would hold its start
// must not hold the skip: the Automation goes past it and the gate speaks for the
// next member that really starts.
func TestAutomationSkipsDeliveredMemberBeforeTheBudgetGate(t *testing.T) {
	a := &Automation{ID: "a1", Name: "chain", Status: AutomationActive, Members: []AutomationMember{
		{ID: "m0", PRDID: "p0", Status: MemberPending},
		{ID: "m1", PRDID: "p1", Status: MemberPending},
	}}
	a.Members[0].OnComplete = "m1"
	snap := map[string]memberInfo{
		"m0": {name: "alpha", shipped: &deliveredNote{prdID: "p0", name: "alpha", release: shippedRelease, commit: shippedCommit},
			verdict: BudgetVerdict{Gate: true, Kind: "ceiling", Reason: "daily ceiling reached"}},
		"m1": {name: "bravo", prdStatus: PRDApproved},
	}
	st := &stepper{a: a, snap: snap, eff: &stepEffects{details: map[string]any{}}, now: time.Now()}
	st.enterAdmitted(&a.Members[0])
	if st.eff.budget != nil || a.BudgetState != nil {
		t.Fatalf("the budget gate must not hold a member that is skipped: %+v", st.eff.budget)
	}
	if a.Members[0].Outcome != "already_shipped" || len(st.eff.delivered) != 1 {
		t.Fatalf("member = %+v delivered = %v, want skipped as already_shipped", a.Members[0], st.eff.delivered)
	}
	if len(st.eff.starts) != 1 || st.eff.starts[0].memberID != "m1" {
		t.Fatalf("starts = %+v, want the member after the delivered one", st.eff.starts)
	}
}
