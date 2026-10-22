package autonomous

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// auditRecords returns the payload of every audit entry written under action,
// in the order they were written.
func (h *coordHarness) auditRecords(action string) []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []map[string]any
	for i, a := range h.audits {
		if a == action {
			out = append(out, h.auditD[i])
		}
	}
	return out
}

// forceState applies fn to the Automation in one write, to lay down a crash
// state the live advance path cannot reach in a single write.
func (h *coordHarness) forceState(a *Automation, fn func(a *Automation)) {
	h.t.Helper()
	if _, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		fn(a)
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
}

// summaryFor returns the status summary of Automation id.
func summaryFor(t *testing.T, h *coordHarness, id string) AutomationStatusSummary {
	t.Helper()
	for _, s := range h.m.Status().Automations {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no status summary for automation %s", id)
	return AutomationStatusSummary{}
}

// TestAutomationReconcile_RestartMidFork: the fork fired before the crash. The
// branch whose start landed keeps running; the branch whose start never landed
// gets exactly one attempt; the join is fed once per branch end.
func TestAutomationReconcile_RestartMidFork(t *testing.T) {
	h := newCoordHarness(t, 4)
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), newBranchStarter()))
	h.startFork(a)
	// The fork fired and both branch starts were issued. Only b1's start landed
	// before the crash, so b1's PRD runs and b2's PRD is still approved.
	h.setStatus(1, PRDRunning)

	rs := &runningStarter{fakeStarter{m: h.m}}
	h.c.starter = rs
	h.c.reconcile()

	if runs, _ := rs.counts(); !reflect.DeepEqual(runs, []string{h.prdID(2)}) {
		t.Fatalf("restart starts = %v, want only the unstarted branch %s", runs, h.prdID(2))
	}
	if m := h.member(a, "b1"); m.Status != MemberRunning {
		t.Fatalf("started branch disturbed: %+v", m)
	}
	if m := h.member(a, "b2"); m.Status != MemberRunning {
		t.Fatalf("unstarted branch not running after its attempt: %+v", m)
	}
	got := h.get(a)
	if got.Status != AutomationActive || !reflect.DeepEqual(got.ActiveCursors, []string{"b1", "b2"}) {
		t.Fatalf("status=%s cursors=%v", got.Status, got.ActiveCursors)
	}
	if join := h.member(a, "j1"); join.Status != MemberRunning {
		t.Fatalf("join fired while branches are live: %+v", join)
	}
	if !h.hasAudit("automation_fork") {
		t.Fatalf("audit = %v, want automation_fork", h.audits)
	}

	// A second pass is idempotent: neither branch starts again.
	h.c.reconcile()
	if runs, _ := rs.counts(); len(runs) != 1 {
		t.Fatalf("second pass started again: runs = %v", runs)
	}

	// Both branches end. The join fires once, even with a duplicate delivery.
	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted)
	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted)
	h.event(2, PRDCompleted)
	joins := h.auditRecords("automation_join")
	if len(joins) != 1 {
		t.Fatalf("join fired %d times, want once", len(joins))
	}
	if joins[0]["actor"] != coordinatorActor || joins[0]["rule"] != BranchAllMustSucceed {
		t.Fatalf("join audit = %+v", joins[0])
	}
	if join := h.member(a, "j1"); join.Status != MemberCompleted {
		t.Fatalf("join not completed: %+v", join)
	}
	if got := h.get(a); got.Status != AutomationCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
}

// TestAutomationReconcile_RestartMidJoin: a join waiting on live branches
// survives the restart. Once every branch is terminal its rule is applied once,
// whether the last end arrives during the restart window or was already
// recorded before the crash.
func TestAutomationReconcile_RestartMidJoin(t *testing.T) {
	t.Run("waiting join survives, then blocks once", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), newBranchStarter()))
		h.startFork(a)
		h.setStatus(1, PRDCompleted) // b1 ended before the crash
		h.event(1, PRDCompleted)
		h.setStatus(2, PRDRunning) // b2 was still running at the crash

		rs := &runningStarter{fakeStarter{m: h.m}}
		h.c.starter = rs
		h.c.reconcile()

		if join := h.member(a, "j1"); join.Status != MemberRunning {
			t.Fatalf("join settled while b2 is live: %+v", join)
		}
		got := h.get(a)
		if got.Status != AutomationActive || !reflect.DeepEqual(got.ActiveCursors, []string{"b2"}) {
			t.Fatalf("status=%s cursors=%v", got.Status, got.ActiveCursors)
		}
		if runs, _ := rs.counts(); len(runs) != 0 {
			t.Fatalf("running branch restarted: %v", runs)
		}
		if n := len(h.auditRecords("automation_join")) + len(h.auditRecords("automation_join_blocked")); n != 0 {
			t.Fatalf("join audited while waiting: %d entries", n)
		}

		// b2 ends in the restart window. The rule is applied once.
		h.setStatus(2, PRDBlocked)
		h.c.reconcile()
		h.event(2, PRDBlocked) // a late live event for the same end must not re-apply
		h.c.reconcile()

		got = h.get(a)
		if got.Status != AutomationBlocked {
			t.Fatalf("status = %s, want blocked", got.Status)
		}
		if join := h.member(a, "j1"); join.Status != MemberBlocked {
			t.Fatalf("join not blocked: %+v", join)
		}
		blocks := h.auditRecords("automation_join_blocked")
		if len(blocks) != 1 {
			t.Fatalf("join blocked %d times, want once", len(blocks))
		}
		if !reflect.DeepEqual(blocks[0]["non_success"], []string{"b2"}) || blocks[0]["actor"] != coordinatorActor {
			t.Fatalf("join_blocked audit = %+v", blocks[0])
		}
		if _, ok := blocks[0]["reason"].(string); !ok {
			t.Fatalf("join_blocked audit has no reason: %+v", blocks[0])
		}
	})

	t.Run("every branch ended before the crash: rule applied once", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), newBranchStarter()))
		h.startFork(a)
		// Both branches ended and the join never applied its rule: the crash fell
		// between the last branch's end and the join's write. No cursor is live.
		h.setStatus(1, PRDCompleted)
		h.setStatus(2, PRDBlocked)
		h.forceState(a, func(a *Automation) {
			a.ActiveCursors = nil
			a.CurrentMemberID = ""
			automationMember(a, "b1").Status = MemberCompleted
			automationMember(a, "b2").Status = MemberBlocked
		})

		rs := &runningStarter{fakeStarter{m: h.m}}
		h.c.starter = rs
		h.c.reconcile()
		h.c.reconcile()

		if got := h.get(a); got.Status != AutomationBlocked {
			t.Fatalf("status = %s, want blocked", got.Status)
		}
		if n := len(h.auditRecords("automation_join_blocked")); n != 1 {
			t.Fatalf("join blocked %d times, want once", n)
		}
		if runs, _ := rs.counts(); len(runs) != 0 {
			t.Fatalf("replay started members: %v", runs)
		}
	})

	t.Run("every branch completed before the crash: join fires once", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, "p1"), newBranchStarter()))
		h.startFork(a)
		h.forceState(a, func(a *Automation) {
			a.ActiveCursors = nil
			a.CurrentMemberID = ""
			automationMember(a, "b1").Status = MemberCompleted
			automationMember(a, "b2").Status = MemberCompleted
		})
		h.setStatus(1, PRDCompleted)
		h.setStatus(2, PRDCompleted)

		rs := &runningStarter{fakeStarter{m: h.m}}
		h.c.starter = rs
		h.c.reconcile()
		h.c.reconcile()

		if n := len(h.auditRecords("automation_join")); n != 1 {
			t.Fatalf("join fired %d times, want once", n)
		}
		if runs, _ := rs.counts(); !reflect.DeepEqual(runs, []string{h.prdID(3)}) {
			t.Fatalf("runs = %v, want the on_complete member once", runs)
		}
		got := h.get(a)
		if got.Status != AutomationActive || !reflect.DeepEqual(got.ActiveCursors, []string{"p1"}) {
			t.Fatalf("status=%s cursors=%v", got.Status, got.ActiveCursors)
		}
		if join := h.member(a, "j1"); join.Status != MemberCompleted {
			t.Fatalf("join not completed: %+v", join)
		}
	})
}

// TestAutomationReconcile_RetryNestedChildStillDraft: a nested member whose child
// is still a draft after the restart gets one activation retry, not a loop.
func TestAutomationReconcile_RetryNestedChildStillDraft(t *testing.T) {
	// setup builds parent -> nested child and starts the nested member before
	// the crash. The pre-crash starter never activates the child, so it stays a
	// draft.
	setup := func(t *testing.T) (*coordHarness, *Automation, *Automation) {
		h := newCoordHarness(t, 8)
		child := h.childAutomation()
		a := h.activateEntry(h.build(h.nestedMembers(child.ID), newBranchStarter()))
		h.startFork(a)
		return h, a, child
	}

	t.Run("draft child gets exactly one retry", func(t *testing.T) {
		h, a, child := setup(t)
		before := len(h.auditRecords("automation_nested_activate"))
		st := newBranchStarter()
		h.c.starter = st

		h.c.reconcile()

		if got := st.activations(); !reflect.DeepEqual(got, []string{child.ID}) {
			t.Fatalf("activations = %v, want one retry of %s", got, child.ID)
		}
		recs := h.auditRecords("automation_nested_activate")
		if len(recs)-before != 1 {
			t.Fatalf("nested activation audited %d times, want once", len(recs)-before)
		}
		last := recs[len(recs)-1]
		if last["member"] != "n1" || last["child_automation_id"] != child.ID || last["actor"] != coordinatorActor {
			t.Fatalf("nested_activate audit = %+v", last)
		}
		if got := h.get(a); got.Status != AutomationActive || h.member(a, "n1").Status != MemberRunning {
			t.Fatalf("parent disturbed by the retry: status=%s member=%+v", got.Status, h.member(a, "n1"))
		}
	})

	t.Run("child that left draft is not retried", func(t *testing.T) {
		h, _, child := setup(t)
		h.forceState(child, func(c *Automation) { c.Status = AutomationActive })
		st := newBranchStarter()
		h.c.starter = st

		h.c.reconcile()

		if got := st.activations(); len(got) != 0 {
			t.Fatalf("activations = %v, want none for an active child", got)
		}
	})

	t.Run("refused retry blocks the parent once", func(t *testing.T) {
		h, a, child := setup(t)
		st := newBranchStarter()
		st.failActivate[child.ID] = errors.New("activation refused")
		h.c.starter = st

		h.c.reconcile()

		if got := h.get(a); got.Status != AutomationBlocked {
			t.Fatalf("status = %s, want blocked by the refused retry", got.Status)
		}
		if !h.hasAudit("automation_blocked") {
			t.Fatalf("audit = %v, want automation_blocked", h.audits)
		}
		recs := h.auditRecords("automation_nested_activate")
		last := recs[len(recs)-1]
		if got, _ := last["result"].(string); !strings.Contains(got, "activation refused") {
			t.Fatalf("nested_activate result = %v, want the refusal", last["result"])
		}
	})
}

// TestAutomationStatusSummary_ActiveCursors: active_cursors lists each live
// cursor sorted by member ID, with prd_id for a PRD member and automation_id for
// a nested one. The legacy current_* fields follow the first cursor when it is a
// running PRD member.
func TestAutomationStatusSummary_ActiveCursors(t *testing.T) {
	t.Run("fork branches in flight", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), newBranchStarter()))
		h.startFork(a)

		s := summaryFor(t, h, a.ID)
		want := []ActiveCursorSummary{
			{MemberID: "b1", PRDID: h.prdID(1), Status: string(MemberRunning)},
			{MemberID: "b2", PRDID: h.prdID(2), Status: string(MemberRunning)},
		}
		if !reflect.DeepEqual(s.ActiveCursors, want) {
			t.Fatalf("active_cursors = %+v, want %+v", s.ActiveCursors, want)
		}
		if s.CurrentMemberID != "b1" || s.CurrentPRDID != h.prdID(1) || s.CurrentPRDTitle != "bravo" {
			t.Fatalf("legacy fields = %q %q %q, want the first cursor b1", s.CurrentMemberID, s.CurrentPRDID, s.CurrentPRDTitle)
		}
	})

	t.Run("nested cursor carries automation_id", func(t *testing.T) {
		h := newCoordHarness(t, 8)
		child := h.childAutomation()
		a := h.activateEntry(h.build(h.nestedMembers(child.ID), newBranchStarter()))
		h.startFork(a)

		s := summaryFor(t, h, a.ID)
		want := []ActiveCursorSummary{{MemberID: "n1", AutomationID: child.ID, Status: string(MemberRunning)}}
		if !reflect.DeepEqual(s.ActiveCursors, want) {
			t.Fatalf("active_cursors = %+v, want %+v", s.ActiveCursors, want)
		}
		// The first cursor is nested, not a PRD member, so the legacy fields stay empty.
		if s.CurrentMemberID != "" || s.CurrentPRDID != "" || s.CurrentPRDTitle != "" {
			t.Fatalf("legacy fields set for a nested first cursor: %q %q %q", s.CurrentMemberID, s.CurrentPRDID, s.CurrentPRDTitle)
		}
	})

	t.Run("idle automation reports an empty list", func(t *testing.T) {
		h := newCoordHarness(t, 1)
		a := h.makeAuto(nil, 1) // a draft: no cursor is live

		s := summaryFor(t, h, a.ID)
		if s.ActiveCursors == nil || len(s.ActiveCursors) != 0 {
			t.Fatalf("active_cursors = %#v, want an empty list", s.ActiveCursors)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"active_cursors":[]`) {
			t.Fatalf("JSON lacks an empty active_cursors: %s", raw)
		}
	})
}

// TestAutomationStatusSummary_JoinWait: join_wait is present only while a join
// waits on its branches, and names the rule, the live branches and the branches
// that ended without success.
func TestAutomationStatusSummary_JoinWait(t *testing.T) {
	t.Run("waiting on live branches", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), newBranchStarter()))
		h.startFork(a)

		s := summaryFor(t, h, a.ID)
		want := &JoinWaitSummary{JoinMemberID: "j1", Rule: BranchAllMustSucceed, PendingBranches: []string{"b1", "b2"}, FailedBranches: []string{}}
		if !reflect.DeepEqual(s.JoinWait, want) {
			t.Fatalf("join_wait = %+v, want %+v", s.JoinWait, want)
		}
	})

	t.Run("one branch ended without success", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), newBranchStarter()))
		h.startFork(a)
		h.setStatus(1, PRDBlocked)
		h.event(1, PRDBlocked)

		s := summaryFor(t, h, a.ID)
		want := &JoinWaitSummary{JoinMemberID: "j1", Rule: BranchAllMustSucceed, PendingBranches: []string{"b2"}, FailedBranches: []string{"b1"}}
		if !reflect.DeepEqual(s.JoinWait, want) {
			t.Fatalf("join_wait = %+v, want %+v", s.JoinWait, want)
		}
	})

	t.Run("any_one rule is named", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAnyOneIsEnough, ""), newBranchStarter()))
		h.startFork(a)

		s := summaryFor(t, h, a.ID)
		if s.JoinWait == nil || s.JoinWait.Rule != BranchAnyOneIsEnough {
			t.Fatalf("join_wait = %+v, want rule %s", s.JoinWait, BranchAnyOneIsEnough)
		}
	})

	t.Run("absent once the join has resolved", func(t *testing.T) {
		h := newCoordHarness(t, 4)
		a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), newBranchStarter()))
		h.startFork(a)
		h.setStatus(1, PRDBlocked)
		h.event(1, PRDBlocked)
		h.setStatus(2, PRDBlocked)
		h.event(2, PRDBlocked)

		s := summaryFor(t, h, a.ID)
		if s.JoinWait != nil {
			t.Fatalf("join_wait = %+v after the join blocked, want absent", s.JoinWait)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "join_wait") {
			t.Fatalf("JSON carries join_wait after the join resolved: %s", raw)
		}
	})
}
