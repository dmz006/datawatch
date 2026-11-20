package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

// Tests for the set_max_parallel and set_stage_gate controls and for inserting
// a decision wait member. They reuse the slot and stage harnesses of the
// max_parallel and stage gate tests: a coordinator with a fake starter that
// records every start.

// wireCoordinator makes h.c the manager's coordinator, as Coordinator.Start
// does, so a control operation can ask it to re-check held members.
// lastDecisionOfKind returns the newest decision of a that has kind. Releasing
// held members adds advance decisions after the control's own, so the control's
// entry is not always the last one.
func lastDecisionOfKind(a *Automation, kind string) (Decision, bool) {
	for i := len(a.Decisions) - 1; i >= 0; i-- {
		if a.Decisions[i].Kind == kind {
			return a.Decisions[i], true
		}
	}
	return Decision{}, false
}

func wireCoordinator(h *slotHarness) {
	h.m.mu.Lock()
	h.m.coordinator = h.c
	h.m.mu.Unlock()
}

func TestSetMaxParallelRaisingTheLimitStartsWaitingMembers(t *testing.T) {
	h, a := newSlotHarness(t, branchIDs(5), 2, 0, BranchAllMustSucceed)
	wireCoordinator(h)
	if got, want := h.waiting(a), []string{"b3", "b4", "b5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waiting before = %v, want %v", got, want)
	}

	cur := h.get(a)
	got, err := h.m.SetMaxParallel(a.ID, "", 4, "alice", cur.Version)
	if err != nil {
		t.Fatalf("SetMaxParallel: %v", err)
	}
	if got.MaxParallel != 4 {
		t.Fatalf("MaxParallel = %d, want 4", got.MaxParallel)
	}
	if want := []string{"b1", "b2", "b3", "b4"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running after raising to 4 = %v, want %v", h.runningIDs(a), want)
	}
	if want := []string{"b5"}; !reflect.DeepEqual(h.waiting(a), want) {
		t.Fatalf("waiting after raising to 4 = %v, want %v", h.waiting(a), want)
	}
	if d, ok := lastDecisionOfKind(got, controlSetMaxParallel); !ok || d.Actor != "alice" || !strings.Contains(d.Note, "set to 4") {
		t.Fatalf("set_max_parallel decision = %+v (found %v)", d, ok)
	}

	// 0 clears the limit and releases the last waiter.
	if _, err := h.m.SetMaxParallel(a.ID, "", 0, "", 0); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if want := []string{"b1", "b2", "b3", "b4", "b5"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running after clearing = %v, want %v", h.runningIDs(a), want)
	}
	if w := h.waiting(a); len(w) != 0 {
		t.Fatalf("still waiting after clearing: %v", w)
	}
	if h.get(a).MaxParallel != 0 {
		t.Fatalf("limit not cleared: %d", h.get(a).MaxParallel)
	}
}

func TestSetMaxParallelLoweringNeverStopsARunningMember(t *testing.T) {
	h, a := newSlotHarness(t, branchIDs(4), 3, 0, BranchAllMustSucceed)
	wireCoordinator(h)
	if want := []string{"b1", "b2", "b3"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running before = %v, want %v", h.runningIDs(a), want)
	}
	if _, err := h.m.SetMaxParallel(a.ID, "", 1, "", 0); err != nil {
		t.Fatalf("SetMaxParallel: %v", err)
	}
	if want := []string{"b1", "b2", "b3"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running after lowering = %v, want all three left running", h.runningIDs(a))
	}
	// The new limit applies to the next start: b4 starts only once the running
	// members are down to the limit, i.e. after b1 and b2 end and b3 runs alone.
	h.end(a, "b1", PRDCompleted, 0)
	h.end(a, "b2", PRDCompleted, 0)
	if want := []string{"b3"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running after b1 and b2 ended = %v, want %v (b4 waits for the limit of 1)", h.runningIDs(a), want)
	}
	h.end(a, "b3", PRDCompleted, 0)
	if want := []string{"b4"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running after b3 ended = %v, want %v", h.runningIDs(a), want)
	}
}

func TestSetMaxParallelWithoutACoordinatorOnlyStoresTheValue(t *testing.T) {
	h, a := newSlotHarness(t, branchIDs(3), 1, 0, BranchAllMustSucceed)
	// The harness never called Start, so no coordinator is wired: the value is
	// stored and nothing else happens.
	if _, err := h.m.SetMaxParallel(a.ID, "", 3, "", 0); err != nil {
		t.Fatalf("SetMaxParallel: %v", err)
	}
	if h.get(a).MaxParallel != 3 {
		t.Fatalf("MaxParallel = %d, want 3", h.get(a).MaxParallel)
	}
	if want := []string{"b1"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running = %v, want %v", h.runningIDs(a), want)
	}
}

func TestSetMaxParallelOnAFork(t *testing.T) {
	h := newCoordHarness(t, 3)
	members := []AutomationMember{
		{ID: "f", Kind: MemberKindFork, ForkMembers: []string{"b1", "b2"}},
		{ID: "b1", PRDID: h.prdID(0)},
		{ID: "b2", PRDID: h.prdID(1)},
		{ID: "j", Kind: MemberKindJoin, JoinMembers: []string{"b1", "b2"}, OnBranchFailure: BranchAllMustSucceed},
		{ID: "tail", PRDID: h.prdID(2)},
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "forked", EntryMemberID: "f", Members: members}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}

	got, err := h.m.SetMaxParallel(a.ID, "f", 1, "", 0)
	if err != nil {
		t.Fatalf("SetMaxParallel on the fork: %v", err)
	}
	if got.MaxParallel != 0 {
		t.Fatalf("the Automation's limit changed: %d", got.MaxParallel)
	}
	if m := automationMember(got, "f"); m == nil || m.MaxParallel != 1 {
		t.Fatalf("fork limit = %+v, want 1", m)
	}
	if last := got.Decisions[len(got.Decisions)-1]; !strings.Contains(last.Note, "fork f") {
		t.Fatalf("decision note = %q, want it to name the fork", last.Note)
	}
	if got, _ = h.m.SetMaxParallel(a.ID, "f", 0, "", 0); automationMember(got, "f").MaxParallel != 0 {
		t.Fatalf("fork limit not cleared")
	}

	// Refusals: a member that is not a fork, an unknown member, a negative limit.
	if _, err := h.m.SetMaxParallel(a.ID, "b1", 1, "", 0); err == nil || !strings.Contains(err.Error(), "only valid on a fork") {
		t.Fatalf("non-fork member error = %v", err)
	}
	if _, err := h.m.SetMaxParallel(a.ID, "nope", 1, "", 0); err == nil || !strings.HasPrefix(err.Error(), "404: ") {
		t.Fatalf("unknown member error = %v", err)
	}
	if _, err := h.m.SetMaxParallel(a.ID, "", -1, "", 0); err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("negative error = %v", err)
	}
	if _, err := h.m.SetMaxParallel("ffffffff", "", 1, "", 0); err == nil || !strings.HasPrefix(err.Error(), "404: ") {
		t.Fatalf("unknown Automation error = %v", err)
	}
	if _, err := h.m.SetMaxParallel(a.ID, "", 1, "", 99); err == nil || !strings.HasPrefix(err.Error(), "409: ") {
		t.Fatalf("stale version error = %v", err)
	}
}

func TestSetMaxParallelAndStageGateRefuseAnEndedAutomation(t *testing.T) {
	h, a := newSlotHarness(t, branchIDs(2), 1, 0, BranchAllMustSucceed)
	if _, err := h.m.CancelAutomation(a.ID, "operator", false, true, h.get(a).Version); err != nil {
		t.Fatalf("CancelAutomation: %v", err)
	}
	if got := h.get(a).Status; got != AutomationCancelled {
		t.Fatalf("status = %s, want cancelled", got)
	}
	if _, err := h.m.SetMaxParallel(a.ID, "", 2, "", 0); err == nil || !strings.HasPrefix(err.Error(), "409: ") {
		t.Fatalf("SetMaxParallel on a cancelled Automation: %v", err)
	}
	if _, err := h.m.SetStageGate(a.ID, true, "", 0); err == nil || !strings.HasPrefix(err.Error(), "409: ") {
		t.Fatalf("SetStageGate on a cancelled Automation: %v", err)
	}
	if h.get(a).MaxParallel != 1 || h.get(a).StageGate {
		t.Fatalf("a refused control changed the Automation: max_parallel=%d stage_gate=%v", h.get(a).MaxParallel, h.get(a).StageGate)
	}
}

func TestSetStageGateTurningItOffReleasesHeldMembers(t *testing.T) {
	h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", bs("b1", "build", "b2", "build", "r1", "review")...)
	wireCoordinator(h)
	if got, want := h.stageWaitingIDs(a), []string{"r1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("held before = %v, want %v", got, want)
	}

	got, err := h.m.SetStageGate(a.ID, false, "alice", h.get(a).Version)
	if err != nil {
		t.Fatalf("SetStageGate off: %v", err)
	}
	if got.StageGate {
		t.Fatal("gate still on")
	}
	if want := []string{"b1", "b2", "r1"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running after the gate went off = %v, want %v", h.runningIDs(a), want)
	}
	if held := h.stageWaitingIDs(a); len(held) != 0 {
		t.Fatalf("still held: %v", held)
	}
	if d, ok := lastDecisionOfKind(got, controlSetStageGate); !ok || d.Actor != "alice" || !strings.Contains(d.Note, "turned off") {
		t.Fatalf("set_stage_gate decision = %+v (found %v)", d, ok)
	}
}

func TestSetStageGateTurningItOnHoldsMembersThatHaveNotStarted(t *testing.T) {
	h, a := newStageHarness(t, false, 1, BranchAllMustSucceed, "", bs("b1", "build", "r1", "review")...)
	wireCoordinator(h)
	// With the gate off and a limit of 1, b1 runs and r1 waits for a slot.
	if got, want := h.runningIDs(a), []string{"b1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running before = %v, want %v", got, want)
	}
	got, err := h.m.SetStageGate(a.ID, true, "", 0)
	if err != nil {
		t.Fatalf("SetStageGate on: %v", err)
	}
	if !got.StageGate {
		t.Fatal("gate still off")
	}
	if want := []string{"r1"}; !reflect.DeepEqual(h.stageWaitingIDs(a), want) {
		t.Fatalf("held after the gate went on = %v, want %v", h.stageWaitingIDs(a), want)
	}
	if want := []string{"b1"}; !reflect.DeepEqual(h.runningIDs(a), want) {
		t.Fatalf("running after the gate went on = %v, want %v", h.runningIDs(a), want)
	}
}

func TestInsertMemberQuestionLandsADecisionWait(t *testing.T) {
	h := newCoordHarness(t, 1)
	a, err := h.m.CreateAutomation(AutomationInput{Name: "draft", EntryMemberID: "e", Members: []AutomationMember{{ID: "e", PRDID: h.prdID(0)}}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}

	got, err := h.m.InsertMember(a.ID, InsertMemberInput{Question: "  Ship the release?  ", Stage: "release", Reason: "sign-off", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatalf("InsertMember: %v", err)
	}
	if len(got.Members) != 2 {
		t.Fatalf("members = %d, want 2", len(got.Members))
	}
	wait := got.Members[1]
	if wait.Kind != MemberKindDecisionWait || wait.Question != "Ship the release?" || wait.Stage != "release" || wait.PRDID != "" {
		t.Fatalf("inserted member = %+v", wait)
	}
	if got.Members[0].OnComplete != wait.ID {
		t.Fatalf("the chain tail leads to %q, want the decision wait %q", got.Members[0].OnComplete, wait.ID)
	}
	if note := got.Decisions[len(got.Decisions)-1].Note; !strings.Contains(note, "decision wait") || !strings.Contains(note, "Ship the release?") {
		t.Fatalf("decision note = %q", note)
	}
}

func TestInsertMemberQuestionGuards(t *testing.T) {
	h := newCoordHarness(t, 2)
	a, err := h.m.CreateAutomation(AutomationInput{Name: "draft", EntryMemberID: "e", Members: []AutomationMember{{ID: "e", PRDID: h.prdID(0)}}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	for name, in := range map[string]InsertMemberInput{
		"nothing named":        {Reason: "x"},
		"blank question only":  {Question: "   ", Reason: "x"},
		"question and PRD":     {Question: "q?", PRDID: h.prdID(1), Reason: "x"},
		"question and an item": {Question: "q?", PendingItemID: "item1", Reason: "x"},
		"question over cap":    {Question: strings.Repeat("q", decisionWaitQuestionMaxBytes+1), Reason: "x"},
	} {
		in.ExpectedVersion = a.Version
		if _, err := h.m.InsertMember(a.ID, in); err == nil || !strings.HasPrefix(err.Error(), "400: ") {
			t.Errorf("%s: error = %v, want a 400", name, err)
		}
	}
	if got, _ := h.m.GetAutomation(a.ID); len(got.Members) != 1 || got.Version != a.Version {
		t.Fatalf("a refused insert changed the Automation: %d members, version %d", len(got.Members), got.Version)
	}
}

func TestInsertMemberQuestionIsHeldOnAStartedAutomation(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.markReady()
	a, err := h.m.CreateAutomation(AutomationInput{Name: "run", EntryMemberID: "e", Members: []AutomationMember{
		{ID: "e", PRDID: h.prdID(0), OnComplete: "n"}, {ID: "n", PRDID: h.prdID(1)},
	}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	a = h.activateEntry(a)

	held, err := h.m.InsertMember(a.ID, InsertMemberInput{Question: "Continue?", Reason: "check in", Actor: "agent:x1", ExpectedVersion: h.get(a).Version})
	if err != nil {
		t.Fatalf("InsertMember: %v", err)
	}
	if len(held.PendingInserts) != 1 || held.PendingInserts[0].Question != "Continue?" || held.PendingInserts[0].PRDID != "" {
		t.Fatalf("pending inserts = %+v", held.PendingInserts)
	}
	if len(held.Members) != 2 {
		t.Fatalf("a held insert joined the graph: %d members", len(held.Members))
	}
	if !strings.Contains(held.PendingInserts[0].subject(), "decision wait") {
		t.Fatalf("subject = %q", held.PendingInserts[0].subject())
	}

	approved, err := h.m.ApproveInsertedMember(a.ID, held.PendingInserts[0].ID, "operator", held.Version)
	if err != nil {
		t.Fatalf("ApproveInsertedMember: %v", err)
	}
	mem := automationMember(approved, held.PendingInserts[0].ID)
	if mem == nil || mem.Kind != MemberKindDecisionWait || mem.Question != "Continue?" {
		t.Fatalf("approved member = %+v", mem)
	}
	if len(approved.PendingInserts) != 0 {
		t.Fatalf("pending inserts after approval = %+v", approved.PendingInserts)
	}
}
