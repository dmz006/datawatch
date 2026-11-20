package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

// Stage gate tests. They sit beside automation_stage_test.go (the stage label)
// and reuse the slot harness of automation_max_parallel_test.go: a coordinator
// with a fake starter that records every start, over a fork of staged branch
// members. Nothing here talks to a real PRD runner or session.

// stageBranch is one fork branch member and the stage it is labelled with ("" is
// unstaged).
type stageBranch struct{ id, stage string }

// newStageHarness builds e0 (slot 0) -> f1 fork over the branches -> j1 join,
// the branches written in the order given (that is the member order), with the
// gate and the Automation's max_parallel set, then ends e0 so the fork fires.
// The fork lists its branches in reverse, so a release that followed the fork
// list instead of member order would show. When nextStage is set the join's
// on_complete names p1, a member of that stage, in the slot after the branches.
func newStageHarness(t *testing.T, gate bool, limit int, rule, nextStage string, branches ...stageBranch) (*slotHarness, *Automation) {
	t.Helper()
	n := len(branches) + 1
	if nextStage != "" {
		n++
	}
	h := &slotHarness{coordHarness: newCoordHarness(t, n), slots: map[string]int{}}
	h.c.markReady()
	h.st = &slotStarter{m: h.m}
	h.c.starter = h.st
	members := []AutomationMember{{ID: "e0", PRDID: h.prdID(0), OnComplete: "f1"}}
	ids := make([]string, len(branches))
	rev := make([]string, len(branches))
	for i, b := range branches {
		h.slots[b.id] = i + 1
		ids[i] = b.id
		rev[len(branches)-1-i] = b.id
		members = append(members, AutomationMember{ID: b.id, PRDID: h.prdID(i + 1), Stage: b.stage})
	}
	join := AutomationMember{ID: "j1", Kind: MemberKindJoin, JoinMembers: ids, OnBranchFailure: rule}
	if nextStage != "" {
		join.OnComplete = "p1"
		h.slots["p1"] = len(branches) + 1
		members = append(members, AutomationMember{ID: "p1", PRDID: h.prdID(len(branches) + 1), Stage: nextStage})
	}
	members = append(members, AutomationMember{ID: "f1", Kind: MemberKindFork, ForkMembers: rev}, join)
	a, err := h.m.CreateAutomation(AutomationInput{Name: "staged", EntryMemberID: "e0", Members: members, MaxParallel: &limit, StageGate: &gate}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if a.StageGate != gate {
		t.Fatalf("created stage_gate = %v, want %v", a.StageGate, gate)
	}
	a = h.activateEntry(a)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	return h, a
}

// stageWaitingIDs lists the members held for an earlier stage, in member order.
func (h *slotHarness) stageWaitingIDs(a *Automation) []string {
	var out []string
	cur := h.get(a)
	for i := range cur.Members {
		if stageWaiting(cur, &cur.Members[i]) != "" {
			out = append(out, cur.Members[i].ID)
		}
	}
	return out
}

// cursorOf returns the status summary entry of the cursor on member id.
func (h *slotHarness) cursorOf(t *testing.T, a *Automation, id string) ActiveCursorSummary {
	t.Helper()
	for _, c := range summaryFor(t, h.coordHarness, a.ID).ActiveCursors {
		if c.MemberID == id {
			return c
		}
	}
	t.Fatalf("no cursor on member %s", id)
	return ActiveCursorSummary{}
}

func bs(pairs ...string) []stageBranch {
	var out []stageBranch
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, stageBranch{id: pairs[i], stage: pairs[i+1]})
	}
	return out
}

// Later-stage members wait for the whole earlier stage: every build member must
// finish before any review member starts, the held members keep their cursors,
// and the join keeps waiting for them.
func TestStageGateLaterStageWaitsForWholeEarlierStage(t *testing.T) {
	h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "b2", "build", "r1", "review", "r2", "review")...)

	if got, want := h.startedIDs(), []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts after the fork = %v, want only the build members %v", got, want)
	}
	if got, want := h.stageWaitingIDs(a), []string{"r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("held for a stage = %v, want %v", got, want)
	}
	got := h.get(a)
	if got.Status != AutomationActive || got.IdleSince != nil {
		t.Fatalf("status=%s idle=%v, want active and not idle (a held member is not blocked)", got.Status, got.IdleSince)
	}
	if want := []string{"b1", "b2", "r1", "r2"}; !reflect.DeepEqual(got.ActiveCursors, want) {
		t.Fatalf("cursors = %v, want every branch held or running: %v", got.ActiveCursors, want)
	}
	for _, id := range []string{"r1", "r2"} {
		if m := h.member(a, id); m.WaitingForStage != "build" || !memberUnstarted(&m) {
			t.Fatalf("%s = %+v, want not started and flagged for build", id, m)
		}
	}
	if !hasCoordDecision(got.Decisions, "waiting") || !h.hasAudit("automation_waiting_for_stage") {
		t.Fatalf("decisions=%+v audits=%v, want a waiting decision and automation_waiting_for_stage", got.Decisions, h.audits)
	}

	// One build member finishing is not enough: the stage still has b2.
	h.end(a, "b1", PRDCompleted, 0)
	if got, want := h.startedIDs(), []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts after b1 = %v, want review still held: %v", got, want)
	}
	if got, want := h.stageWaitingIDs(a), []string{"r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("held after b1 = %v, want %v", got, want)
	}

	// The last build member releases both review members, in member order.
	h.end(a, "b2", PRDCompleted, 0)
	if got, want := h.startedIDs(), []string{"b1", "b2", "r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start order = %v, want %v", got, want)
	}
	if w := h.stageWaitingIDs(a); len(w) != 0 {
		t.Fatalf("still held after the stage finished: %v", w)
	}
	for _, id := range []string{"r1", "r2"} {
		if m := h.member(a, id); m.WaitingForStage != "" || m.Status != MemberRunning {
			t.Fatalf("%s = %+v, want running with the flag cleared", id, m)
		}
	}
	if j := h.member(a, "j1"); j.Status != MemberRunning {
		t.Fatalf("join = %+v, want it to keep waiting for the review members", j)
	}

	h.end(a, "r1", PRDCompleted, 0)
	h.end(a, "r2", PRDCompleted, 0)
	if j := h.member(a, "j1"); j.Status != MemberCompleted {
		t.Fatalf("join = %+v, want fired", j)
	}
	if fin := h.get(a); fin.Status != AutomationCompleted {
		t.Fatalf("status = %s, want completed", fin.Status)
	}
}

// With the gate off, or never set, every member starts at once as before: the
// stage is only a label.
func TestStageGateOffKeepsTodaysBehavior(t *testing.T) {
	h, a := newStageHarness(t, false, 0, BranchAllMustSucceed, "", bs("b1", "build", "b2", "build", "r1", "review", "r2", "review")...)
	if got, want := h.startedIDs(), []string{"b1", "b2", "r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts with the gate off = %v, want all four at once: %v", got, want)
	}
	if w := h.stageWaitingIDs(a); len(w) != 0 {
		t.Fatalf("held with the gate off: %v", w)
	}
	if h.hasAudit("automation_waiting_for_stage") {
		t.Fatalf("audits = %v, want no stage wait with the gate off", h.audits)
	}
	s := summaryFor(t, h.coordHarness, a.ID)
	if s.StageGate || len(s.WaitingForStage) != 0 {
		t.Fatalf("summary stage_gate=%v waiting=%v, want neither with the gate off", s.StageGate, s.WaitingForStage)
	}
	for _, id := range []string{"b1", "b2", "r1", "r2"} {
		h.end(a, id, PRDCompleted, 0)
	}
	if fin := h.get(a); fin.Status != AutomationCompleted {
		t.Fatalf("status = %s, want completed", fin.Status)
	}

	// A record from before the field existed has no stage_gate and behaves the same.
	var zero Automation
	if zero.StageGate {
		t.Fatal("the zero value must leave the gate off")
	}
	member := &AutomationMember{ID: "x", Stage: "review"}
	if got := stageHold(&Automation{Members: []AutomationMember{{ID: "y", Stage: "build"}, *member}}, member); got != "" {
		t.Fatalf("stageHold with the gate unset = %q, want none", got)
	}
}

// Unstaged members form no gate and are never held: one starts at once whatever
// is unfinished, and a running unstaged member does not hold a later stage.
func TestStageGateUnstagedMembersIgnoreGate(t *testing.T) {
	// u1 is written first and is unstaged: it does not make a first stage.
	h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("u1", "", "b1", "build", "r1", "review", "u2", "")...)
	if got, want := h.startedIDs(), []string{"u1", "b1", "u2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want both unstaged members and the first stage: %v", got, want)
	}
	if got, want := h.stageWaitingIDs(a), []string{"r1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("held = %v, want only r1", got)
	}
	// The unstaged members are still running when the first stage ends, and the
	// review member starts anyway.
	h.end(a, "b1", PRDCompleted, 0)
	if got, want := h.startedIDs(), []string{"u1", "b1", "u2", "r1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts after b1 = %v, want r1 released while u1 and u2 run: %v", got, want)
	}
	for _, id := range []string{"u1", "u2"} {
		if m := h.member(a, id); m.Status != MemberRunning || m.WaitingForStage != "" {
			t.Fatalf("%s = %+v, want running", id, m)
		}
	}
}

// Stages are ordered by first appearance in the member list, not by name: here
// review appears first, so build waits for it.
func TestStageGateStageOrderIsFirstAppearance(t *testing.T) {
	h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("r1", "review", "b1", "build", "r2", "review")...)
	if got, want := h.startedIDs(), []string{"r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want the first-appearing stage (review) %v", got, want)
	}
	if got, want := h.stageWaitingIDs(a), []string{"b1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("held = %v, want b1", got)
	}
	if c := h.cursorOf(t, a, "b1"); c.WaitStage != "review" || c.WaitReason != "waiting for stage review" {
		t.Fatalf("cursor b1 = %+v, want waiting for stage review", c)
	}
	h.end(a, "r1", PRDCompleted, 0)
	h.end(a, "r2", PRDCompleted, 0)
	if got, want := h.startedIDs(), []string{"r1", "r2", "b1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want %v", got, want)
	}
}

// With three stages a held member names the first unfinished stage before it,
// and the name follows as the stages finish.
func TestStageGateHoldNamesFirstUnfinishedStage(t *testing.T) {
	h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "r1", "review", "s1", "ship")...)
	for _, id := range []string{"r1", "s1"} {
		if c := h.cursorOf(t, a, id); c.Status != CursorStatusWaitingForStage || c.WaitStage != "build" {
			t.Fatalf("cursor %s = %+v, want waiting for stage build", id, c)
		}
	}
	h.end(a, "b1", PRDCompleted, 0)
	if got, want := h.startedIDs(), []string{"b1", "r1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts after build = %v, want only review: %v", got, want)
	}
	c := h.cursorOf(t, a, "s1")
	if c.Status != CursorStatusWaitingForStage || c.WaitStage != "review" || c.WaitReason != "waiting for stage review" {
		t.Fatalf("cursor s1 = %+v, want the wait moved on to review", c)
	}
	if m := h.member(a, "s1"); m.WaitingForStage != "review" {
		t.Fatalf("s1 flag = %q, want review (the stored flag follows the stage)", m.WaitingForStage)
	}
	h.end(a, "r1", PRDCompleted, 0)
	if got, want := h.startedIDs(), []string{"b1", "r1", "s1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want %v", got, want)
	}
}

// The status data reads a held member as waiting for the stage, not blocked, and
// clears it on release.
func TestStageGateStatusReadsWaitingForStage(t *testing.T) {
	h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "r1", "review")...)
	s := summaryFor(t, h.coordHarness, a.ID)
	if !s.StageGate {
		t.Fatalf("summary stage_gate = false, want true")
	}
	if s.Status != string(AutomationActive) || s.BlockedReason != "" {
		t.Fatalf("summary status=%q blocked=%q, want active and not blocked", s.Status, s.BlockedReason)
	}
	if want := []string{"r1"}; !reflect.DeepEqual(s.WaitingForStage, want) {
		t.Fatalf("waiting_for_stage = %v, want %v", s.WaitingForStage, want)
	}
	c := h.cursorOf(t, a, "r1")
	if c.Status != CursorStatusWaitingForStage || c.WaitReason != "waiting for stage build" || c.WaitStage != "build" {
		t.Fatalf("cursor r1 = %+v, want waiting for stage build", c)
	}
	if strings.Contains(c.Status, "block") || strings.Contains(c.WaitReason, "block") {
		t.Fatalf("cursor r1 reads as blocked: %+v", c)
	}
	if c := h.cursorOf(t, a, "b1"); c.Status != string(MemberRunning) || c.WaitReason != "" || c.WaitStage != "" {
		t.Fatalf("cursor b1 = %+v, want running", c)
	}
	h.end(a, "b1", PRDCompleted, 0)
	s = summaryFor(t, h.coordHarness, a.ID)
	if len(s.WaitingForStage) != 0 {
		t.Fatalf("waiting_for_stage = %v after the stage finished, want none", s.WaitingForStage)
	}
	if c := h.cursorOf(t, a, "r1"); c.WaitReason != "" || c.Status != string(MemberRunning) {
		t.Fatalf("cursor r1 = %+v, want running and no longer waiting", c)
	}
}

// The gate composes with max_parallel: a released member that finds every slot
// full waits for a slot (not for the stage), and the limit holds throughout.
func TestStageGateComposesWithMaxParallel(t *testing.T) {
	h, a := newStageHarness(t, true, 1, BranchAllMustSucceed, "", bs("b1", "build", "b2", "build", "r1", "review", "r2", "review")...)

	if got, want := h.runningIDs(a), []string{"b1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after the fork = %v, want one slot filled: %v", got, want)
	}
	if got, want := h.waiting(a), []string{"b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waiting for a slot = %v, want b2", got)
	}
	if got, want := h.stageWaitingIDs(a), []string{"r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("held for a stage = %v, want r1 r2", got)
	}
	if c := h.cursorOf(t, a, "b2"); c.Status != CursorStatusWaitingForSlot || c.WaitReason != SlotWaitReason {
		t.Fatalf("cursor b2 = %+v, want waiting for a free slot", c)
	}
	if c := h.cursorOf(t, a, "r1"); c.Status != CursorStatusWaitingForStage {
		t.Fatalf("cursor r1 = %+v, want waiting for a stage", c)
	}

	// b1 ends: its slot goes to b2, and the stage is still unfinished.
	h.end(a, "b1", PRDCompleted, 1)
	if got, want := h.runningIDs(a), []string{"b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after b1 = %v, want b2", got)
	}
	if got, want := h.stageWaitingIDs(a), []string{"r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("held after b1 = %v, want r1 r2", got)
	}

	// b2 ends: the stage is done, r1 takes the slot, r2 is released but finds it full.
	h.end(a, "b2", PRDCompleted, 1)
	if got, want := h.runningIDs(a), []string{"r1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after b2 = %v, want r1 only", got)
	}
	if w := h.stageWaitingIDs(a); len(w) != 0 {
		t.Fatalf("held for a stage after the stage finished = %v, want none", w)
	}
	if got, want := h.waiting(a), []string{"r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waiting for a slot = %v, want r2", got)
	}
	if m := h.member(a, "r2"); m.WaitingForStage != "" || !m.WaitingForSlot {
		t.Fatalf("r2 = %+v, want waiting for a slot with the stage flag cleared", m)
	}
	if c := h.cursorOf(t, a, "r2"); c.Status != CursorStatusWaitingForSlot {
		t.Fatalf("cursor r2 = %+v, want waiting for a free slot", c)
	}

	h.end(a, "r1", PRDCompleted, 1)
	if got, want := h.runningIDs(a), []string{"r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after r1 = %v, want r2", got)
	}
	h.end(a, "r2", PRDCompleted, 1)
	if want := []string{"b1", "b2", "r1", "r2"}; !reflect.DeepEqual(h.startedIDs(), want) {
		t.Fatalf("start order = %v, want %v", h.startedIDs(), want)
	}
	if peak := h.st.peakLive(); peak != 1 {
		t.Fatalf("peak members live at once = %d, want 1", peak)
	}
	if fin := h.get(a); fin.Status != AutomationCompleted {
		t.Fatalf("status = %s, want completed", fin.Status)
	}
}

// Only a member that is completed, failed or skipped finishes a stage. A failed
// member releases the next stage; a blocked one that the flow has not moved past
// still holds it.
func TestStageGateFinishedMeansCompletedFailedOrSkipped(t *testing.T) {
	t.Run("failed releases", func(t *testing.T) {
		h, a := newStageHarness(t, true, 0, BranchContinueAnyway, "", bs("b1", "build", "b2", "build", "r1", "review")...)
		h.end(a, "b1", PRDFailed, 0)
		h.end(a, "b2", PRDCompleted, 0)
		if got, want := h.startedIDs(), []string{"b1", "b2", "r1"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("starts = %v, want r1 released once b1 failed and b2 completed: %v", got, want)
		}
	})
	t.Run("blocked still holds", func(t *testing.T) {
		h, a := newStageHarness(t, true, 0, BranchContinueAnyway, "", bs("b1", "build", "b2", "build", "r1", "review")...)
		h.end(a, "b1", PRDBlocked, 0)
		h.end(a, "b2", PRDCompleted, 0)
		if got, want := h.startedIDs(), []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("starts = %v, want r1 held while a build member is blocked: %v", got, want)
		}
		if got, want := h.stageWaitingIDs(a), []string{"r1"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("held = %v, want r1", got)
		}
	})
	t.Run("skipped releases", func(t *testing.T) {
		h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "b2", "build", "r1", "review")...)
		h.m.coordinator = h.c // as after Coordinator.Start
		h.end(a, "b1", PRDCompleted, 0)
		// The operator skips the build member that is still running.
		if _, err := h.m.SkipMember(a.ID, "b2", edgeOnComplete, "operator", h.get(a).Version); err != nil {
			t.Fatalf("SkipMember: %v", err)
		}
		if got, want := h.startedIDs(), []string{"b1", "b2", "r1"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("starts = %v, want r1 released once b2 was skipped: %v", got, want)
		}
	})
}

// A member that can no longer run never holds a stage open: after an
// any_one_is_enough join fires, the cancelled and skipped branches do not hold
// the member behind the join.
func TestStageGateCancelledBranchesDoNotHoldNextStage(t *testing.T) {
	h, a := newStageHarness(t, true, 0, BranchAnyOneIsEnough, "ship", bs("b1", "build", "b2", "build", "r1", "review")...)
	// b1 is a build member, so b2 runs and r1 is held; b1 completing fires the join.
	h.end(a, "b1", PRDCompleted, 0)
	got := h.get(a)
	if j := automationMember(got, "j1"); j.Status != MemberCompleted {
		t.Fatalf("join = %+v, want fired", j)
	}
	if b2 := automationMember(got, "b2"); b2.Status != MemberCancelled {
		t.Fatalf("b2 = %+v, want cancelled by the join", b2)
	}
	if r1 := automationMember(got, "r1"); r1.Status != MemberSkipped || r1.WaitingForStage != "" {
		t.Fatalf("r1 = %+v, want skipped with no stage flag left", r1)
	}
	if want := []string{"b1", "b2", "p1"}; !reflect.DeepEqual(h.startedIDs(), want) {
		t.Fatalf("starts = %v, want p1 to start behind the join: %v", h.startedIDs(), want)
	}
	if p1 := automationMember(got, "p1"); p1.Status != MemberRunning || p1.WaitingForStage != "" {
		t.Fatalf("p1 = %+v, want running (the cancelled and skipped members hold no stage)", p1)
	}
}

// A member is never held for a member that can only run after it or that the
// flow can no longer reach: waiting would hold it forever.
func TestStageGateDoesNotWaitForMembersItCannotOutrun(t *testing.T) {
	t.Run("a later member of an earlier stage", func(t *testing.T) {
		// m0 (build) -> m1 (review) -> m2 (build): m2 is behind m1, so m1 does not
		// wait for it and the chain runs through.
		h := newCoordHarness(t, 3)
		h.c.markReady()
		gate := true
		a, err := h.m.CreateAutomation(AutomationInput{Name: "misordered", StageGate: &gate, Members: []AutomationMember{
			{ID: "m0", PRDID: h.prds[0].ID, Stage: "build", OnComplete: "m1"},
			{ID: "m1", PRDID: h.prds[1].ID, Stage: "review", OnComplete: "m2"},
			{ID: "m2", PRDID: h.prds[2].ID, Stage: "build"},
		}}, "operator")
		if err != nil {
			t.Fatal(err)
		}
		a = h.force(a)
		for i := 0; i < 3; i++ {
			h.setStatus(i, PRDCompleted)
			h.event(i, PRDCompleted)
			if i < 2 {
				if m := h.member(a, "m"+string(rune('1'+i))); m.Status != MemberRunning || m.WaitingForStage != "" {
					t.Fatalf("m%d = %+v, want running, not held", i+1, m)
				}
			}
		}
		if fin := h.get(a); fin.Status != AutomationCompleted {
			t.Fatalf("status = %s, want completed", fin.Status)
		}
	})
	t.Run("a handler the flow did not take", func(t *testing.T) {
		// m0 (build) completes to m2 (review); m1 (build) is its on_failed handler
		// and nothing reaches it, so it does not hold review.
		h := newCoordHarness(t, 3)
		h.c.markReady()
		gate := true
		a, err := h.m.CreateAutomation(AutomationInput{Name: "handler", StageGate: &gate, Members: []AutomationMember{
			{ID: "m0", PRDID: h.prds[0].ID, Stage: "build", OnComplete: "m2", OnFailed: "m1"},
			{ID: "m1", PRDID: h.prds[1].ID, Stage: "build"},
			{ID: "m2", PRDID: h.prds[2].ID, Stage: "review"},
		}}, "operator")
		if err != nil {
			t.Fatal(err)
		}
		a = h.force(a)
		h.setStatus(0, PRDCompleted)
		h.event(0, PRDCompleted)
		if m := h.member(a, "m2"); m.Status != MemberRunning || m.WaitingForStage != "" {
			t.Fatalf("m2 = %+v, want running: the untaken handler holds nothing", m)
		}
	})
}

// Restart: members held for a stage survive the daemon restart through
// reconcile. Nothing starts while the earlier stage is unfinished and nothing is
// blocked; an earlier stage that finished while the daemon was down releases the
// held members in member order, once.
func TestStageGateReconcileResumesHeldMembers(t *testing.T) {
	t.Run("stage still unfinished", func(t *testing.T) {
		h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "b2", "build", "r1", "review", "r2", "review")...)
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		h.c.reconcile()
		if got := rs.started(); len(got) != 0 {
			t.Fatalf("reconcile started %v while the stage is unfinished", got)
		}
		got := h.get(a)
		if got.Status != AutomationActive {
			t.Fatalf("status = %s, want active (waiting is not blocked)", got.Status)
		}
		if w := h.stageWaitingIDs(a); !reflect.DeepEqual(w, []string{"r1", "r2"}) {
			t.Fatalf("held after restart = %v, want r1 r2 kept", w)
		}
		if want := []string{"b1", "b2", "r1", "r2"}; !reflect.DeepEqual(got.ActiveCursors, want) {
			t.Fatalf("cursors after restart = %v, want %v", got.ActiveCursors, want)
		}
		if m := h.member(a, "r1"); m.WaitingForStage != "build" {
			t.Fatalf("r1 flag = %q after restart, want build", m.WaitingForStage)
		}
	})

	t.Run("the earlier stage ended while the daemon was down", func(t *testing.T) {
		h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "b2", "build", "r1", "review", "r2", "review")...)
		// Both build PRDs completed during the downtime and their events were lost.
		h.setStatus(h.slots["b1"], PRDCompleted)
		h.setStatus(h.slots["b2"], PRDCompleted)
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		if got, want := rs.started(), []string{h.prdID(h.slots["r1"]), h.prdID(h.slots["r2"])}; !reflect.DeepEqual(got, want) {
			t.Fatalf("reconcile started %v, want the held members in member order %v", got, want)
		}
		if got, want := h.runningIDs(a), []string{"r1", "r2"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("running = %v, want %v", got, want)
		}
		h.c.reconcile() // idempotent
		if got := rs.started(); len(got) != 2 {
			t.Fatalf("second pass started again: %v", got)
		}
		if w := h.stageWaitingIDs(a); len(w) != 0 {
			t.Fatalf("still held after reconcile: %v", w)
		}
	})

	t.Run("the crash fell between the end of the stage and the release", func(t *testing.T) {
		h, a := newStageHarness(t, true, 0, BranchContinueAnyway, "", bs("d", "build", "c", "build", "b", "review", "a", "review")...)
		// Lay down the stored state: the build members ended and left the cursor
		// set, the review members still hold their cursors and flags, nothing runs.
		h.forceState(a, func(a *Automation) {
			for _, id := range []string{"d", "c"} {
				m := automationMember(a, id)
				m.Status = MemberCompleted
				dropCursor(a, id)
			}
		})
		h.setStatus(h.slots["d"], PRDCompleted)
		h.setStatus(h.slots["c"], PRDCompleted)
		if w := h.stageWaitingIDs(a); len(w) != 0 {
			t.Fatalf("held before restart = %v, want the gate already open", w)
		}
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		if got, want := rs.started(), []string{h.prdID(h.slots["b"]), h.prdID(h.slots["a"])}; !reflect.DeepEqual(got, want) {
			t.Fatalf("reconcile started %v, want the held members in member order %v", got, want)
		}
		for _, id := range []string{"b", "a"} {
			if m := h.member(a, id); m.Status != MemberRunning || m.WaitingForStage != "" {
				t.Fatalf("%s = %+v, want running with the flag cleared", id, m)
			}
		}
		h.c.reconcile()
		if got := rs.started(); len(got) != 2 {
			t.Fatalf("second pass started again: %v", got)
		}
	})

	t.Run("a member held while the Automation was paused is flagged on resume", func(t *testing.T) {
		h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "r1", "review")...)
		// Lay down a held member that was never flagged, as a pause leaves it.
		h.forceState(a, func(a *Automation) { automationMember(a, "r1").WaitingForStage = "" })
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		if got := rs.started(); len(got) != 0 {
			t.Fatalf("reconcile started %v while the stage is unfinished", got)
		}
		if m := h.member(a, "r1"); m.WaitingForStage != "build" {
			t.Fatalf("r1 flag = %q, want build written by the reconcile pass", m.WaitingForStage)
		}
		v := h.get(a).Version
		h.c.reconcile()
		if got := h.get(a).Version; got != v {
			t.Fatalf("a second pass wrote the Automation (version %d -> %d), want no write", v, got)
		}
		// Ending b1 now releases r1 through the stepper.
		h.setStatus(h.slots["b1"], PRDCompleted)
		h.event(h.slots["b1"], PRDCompleted)
		if got, want := rs.started(), []string{h.prdID(h.slots["r1"])}; !reflect.DeepEqual(got, want) {
			t.Fatalf("started %v, want r1 released by the end of the stage", got)
		}
	})
}

// The field and the held members persist through the store, so a daemon restart
// reloads a held member as held.
func TestStageGatePersistsThroughStore(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{"p1": {ID: "p1"}, "p2": {ID: "p2"}}
	s1, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatal(err)
	}
	created, err := s1.Create(&Automation{
		Name:          "gated",
		StageGate:     true,
		EntryMemberID: "m1",
		Members: []AutomationMember{
			{ID: "m1", PRDID: "p1", Stage: "build", OnComplete: "m2"},
			{ID: "m2", PRDID: "p2", Stage: "review"},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s1.Update(created.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		automationMember(a, "m2").WaitingForStage = "build"
		a.ActiveCursors = []string{"m1", "m2"}
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	s2, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get(created.ID)
	if !ok {
		t.Fatal("reloaded store lost the Automation")
	}
	if !got.StageGate {
		t.Fatal("stage_gate lost on reload")
	}
	if m := automationMember(got, "m2"); m.WaitingForStage != "build" {
		t.Fatalf("m2 = %+v, want held for build after reload", m)
	}
}

// The manager takes stage_gate on create and update; an update that omits it
// keeps the stored value, and false turns it off.
func TestStageGateManagerCreateUpdate(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prds := makeAutomationMemberPRDs(t, m, 2)
	members := func(i int) []AutomationMember {
		return []AutomationMember{{ID: "m0", PRDID: prds[i].ID, Stage: []string{"build", "ship"}[i]}} // one stage name each: stage tags are per daemon
	}

	plain, err := m.CreateAutomation(AutomationInput{Name: "plain", Members: members(0)}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if plain.StageGate {
		t.Fatal("stage_gate must default to false")
	}

	on := true
	a, err := m.CreateAutomation(AutomationInput{Name: "gated", Members: members(1), StageGate: &on}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !a.StageGate {
		t.Fatal("create dropped stage_gate")
	}
	a, err = m.UpdateAutomation(a.ID, a.Version, AutomationInput{Name: "renamed"}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !a.StageGate {
		t.Fatal("an update that omits stage_gate must keep it")
	}
	off := false
	a, err = m.UpdateAutomation(a.ID, a.Version, AutomationInput{Name: "renamed", StageGate: &off}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if a.StageGate {
		t.Fatal("stage_gate:false must turn the gate off")
	}
}
