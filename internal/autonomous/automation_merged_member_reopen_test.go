package autonomous

import "testing"

// reopenBlocked cancels member 0 of a two-member chain so the Automation goes
// blocked on it, the state a reopened member must be followed out of.
func reopenBlocked(t *testing.T) (*coordHarness, *Automation) {
	t.Helper()
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)
	got := h.get(a)
	if got.Status != AutomationBlocked || got.Members[0].Status != MemberBlocked || got.CurrentMemberID != "m0" {
		t.Fatalf("setup: status=%s member=%s cursor=%q, want blocked on m0", got.Status, got.Members[0].Status, got.CurrentMemberID)
	}
	return h, a
}

func TestMergedMemberReopen_CancelledThenRunAgainResumesChain(t *testing.T) {
	h, a := reopenBlocked(t)
	h.setStatus(0, PRDRunning)
	h.event(0, PRDRunning)

	got := h.get(a)
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
	m0 := got.Members[0]
	if m0.Status != MemberRunning || m0.Outcome != "" || m0.EdgeTaken != "" || m0.FinishedAt != nil {
		t.Fatalf("member 0 = %+v, want running with no outcome", m0)
	}
	if got.CurrentMemberID != "m0" || got.IdleSince != nil {
		t.Fatalf("cursor=%q idle=%v, want m0 and not idle", got.CurrentMemberID, got.IdleSince)
	}
	var d *Decision
	for i := range got.Decisions {
		if got.Decisions[i].Kind == decisionReopened {
			d = &got.Decisions[i]
		}
	}
	if d == nil || d.Actor != coordinatorActor {
		t.Fatalf("reopened decision = %+v, want one by %s", d, coordinatorActor)
	}
	if !h.hasAudit("automation_reopened") {
		t.Fatal("no automation_reopened audit")
	}
	if runs, resumes := h.fake.counts(); len(runs) != 0 || len(resumes) != 0 {
		t.Fatalf("reopen started members: runs=%v resumes=%v, want none", runs, resumes)
	}

	// The chain carries on from the reopened member.
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	got = h.get(a)
	if got.Members[0].Status != MemberCompleted || got.CurrentMemberID != "m1" || got.Status != AutomationActive {
		t.Fatalf("after completion: member=%s cursor=%q status=%s, want completed, m1, active",
			got.Members[0].Status, got.CurrentMemberID, got.Status)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("starter runs = %v, want [%s]", runs, h.prds[1].ID)
	}
}

func TestMergedMemberReopen_RepeatedRunningEventIsNoOp(t *testing.T) {
	h, a := reopenBlocked(t)
	h.setStatus(0, PRDRunning)
	h.event(0, PRDRunning)
	first := h.get(a)
	h.event(0, PRDRunning)
	second := h.get(a)
	if second.Version != first.Version || len(second.Decisions) != len(first.Decisions) {
		t.Fatalf("second running event wrote: version %d -> %d, decisions %d -> %d",
			first.Version, second.Version, len(first.Decisions), len(second.Decisions))
	}
}

func TestMergedMemberReopen_StartInFlightIsNotReopened(t *testing.T) {
	h, a := reopenBlocked(t)
	release := h.c.beginStart(h.prds[0].ID, true)
	defer release()
	h.setStatus(0, PRDRunning)
	h.event(0, PRDRunning)
	if got := h.get(a); got.Status != AutomationBlocked || got.Members[0].Status != MemberBlocked {
		t.Fatalf("status=%s member=%s, want the in-flight start left alone", got.Status, got.Members[0].Status)
	}
}

func TestMergedMemberReopen_PausedAutomationStaysPaused(t *testing.T) {
	h, a := reopenBlocked(t)
	h.forceState(a, func(a *Automation) { a.Status = AutomationPaused })
	before := h.get(a)
	h.setStatus(0, PRDRunning)
	h.event(0, PRDRunning)

	got := h.get(a)
	if got.Status != AutomationPaused || got.Members[0].Status != MemberBlocked {
		t.Fatalf("status=%s member=%s, want paused with the member untouched", got.Status, got.Members[0].Status)
	}
	if got.Version != before.Version || hasCoordDecision(got.Decisions, decisionReopened) {
		t.Fatalf("paused Automation was written: version %d -> %d, decisions %+v", before.Version, got.Version, got.Decisions)
	}
	if h.hasAudit("automation_reopened") {
		t.Fatal("automation_reopened audit on a paused Automation")
	}
}

func TestMergedMemberReopen_CancelledAutomationStaysCancelled(t *testing.T) {
	h, a := reopenBlocked(t)
	h.forceState(a, func(a *Automation) { a.Status = AutomationCancelled })
	before := h.get(a)
	h.setStatus(0, PRDRunning)
	h.event(0, PRDRunning)

	got := h.get(a)
	if got.Status != AutomationCancelled || got.Members[0].Status != MemberBlocked {
		t.Fatalf("status=%s member=%s, want cancelled with the member untouched", got.Status, got.Members[0].Status)
	}
	if got.Version != before.Version || hasCoordDecision(got.Decisions, decisionReopened) {
		t.Fatalf("cancelled Automation was written: version %d -> %d, decisions %+v", before.Version, got.Version, got.Decisions)
	}
}

func TestMergedMemberReopen_CompletedAutomationStaysCompleted(t *testing.T) {
	h, a := reopenBlocked(t)
	h.forceState(a, func(a *Automation) { a.Status = AutomationCompleted })
	h.setStatus(0, PRDRunning)
	h.event(0, PRDRunning)
	if got := h.get(a); got.Status != AutomationCompleted || hasCoordDecision(got.Decisions, decisionReopened) {
		t.Fatalf("status=%s decisions=%+v, want completed and not reopened", got.Status, got.Decisions)
	}
}

func TestMergedMemberReopen_BlockedByFailureIsNotReopened(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.SetReady()
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(0, PRDFailed)
	h.event(0, PRDFailed)
	if got := h.get(a); got.Status != AutomationBlocked {
		t.Fatalf("setup: status=%s, want blocked", got.Status)
	}
	h.setStatus(0, PRDRunning)
	h.event(0, PRDRunning)
	if got := h.get(a); got.Status != AutomationBlocked || got.Members[0].Status != MemberFailed {
		t.Fatalf("status=%s member=%s, want a failed member left blocked", got.Status, got.Members[0].Status)
	}
}
