package autonomous

// Feature presence tests: one test per Automation feature, each failing if the
// feature is lost. Each one calls the real function or drives the real control
// and asserts an observable result. They reuse the fixtures of the feature's own
// test file (the coordinator and slot/stage harnesses) and never a fixed
// calendar date: any time value is an offset from time.Now.

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// presenceChain is a fork over two branches, a1->a2 and c1->c2, with the join
// j1 over the two tails, then p5. The fork lists its heads out of order.
func presenceChain() *Automation {
	return &Automation{
		ID:        "presence-chain",
		Name:      "presence",
		CreatedAt: time.Now().Add(-time.Hour),
		Members: []AutomationMember{
			{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"c1", "a1"}},
			{ID: "a1", Kind: MemberKindPRD, PRDID: "p1", OnComplete: "a2"},
			{ID: "a2", Kind: MemberKindPRD, PRDID: "p2"},
			{ID: "c1", Kind: MemberKindPRD, PRDID: "p3", OnComplete: "c2"},
			{ID: "c2", Kind: MemberKindPRD, PRDID: "p4"},
			{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"c2", "a2"}, OnComplete: "p5"},
			{ID: "p5", Kind: MemberKindPRD, PRDID: "p5"},
		},
	}
}

// TestAutomationFeaturePresence_BranchChain: a fork branch is a chain of members
// followed along on_complete, with the tail the join collects.
func TestAutomationFeaturePresence_BranchChain(t *testing.T) {
	a := presenceChain()
	f := &a.Members[0]

	if got := walkBranchChain(a, "a1"); len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Fatalf("walkBranchChain(a1) = %v, want [a1 a2]", got)
	}
	if got := walkBranchChain(a, "nope"); len(got) != 0 {
		t.Fatalf("walkBranchChain(unknown) = %v, want empty", got)
	}

	chains := forkChains(a, f)
	want := [][]string{{"a1", "a2"}, {"c1", "c2"}}
	if len(chains) != 2 || !reflect.DeepEqual(chains[0], want[0]) || !reflect.DeepEqual(chains[1], want[1]) {
		t.Fatalf("forkChains = %v, want %v (sorted head order)", chains, want)
	}
	if tails := forkTails(a, f); !reflect.DeepEqual(tails, []string{"a2", "c2"}) {
		t.Fatalf("forkTails = %v, want [a2 c2]", tails)
	}

	fork, join, chain := branchChainOf(a, "c2")
	if fork == nil || fork.ID != "f1" || join == nil || join.ID != "j1" || !reflect.DeepEqual(chain, []string{"c1", "c2"}) {
		t.Fatalf("branchChainOf(c2) = fork %v join %v chain %v, want f1 / j1 / [c1 c2]", fork, join, chain)
	}
	if !isBranchTail(chain, "c2") || isBranchTail(chain, "c1") {
		t.Fatal("isBranchTail must mark only the last chain member")
	}
	if fork, _, _ := branchChainOf(a, "p5"); fork != nil {
		t.Fatalf("branchChainOf(p5) found fork %s; p5 is not on a branch", fork.ID)
	}
	if got := forkForJoin(a, &a.Members[5]); got == nil || got.ID != "f1" {
		t.Fatalf("forkForJoin(j1) = %v, want f1", got)
	}

	// A loop in on_complete never repeats a member.
	loop := presenceChain()
	loop.Members[2].OnComplete = "a1"
	if got := walkBranchChain(loop, "a1"); len(got) != 2 {
		t.Fatalf("walkBranchChain over a loop = %v, want two members", got)
	}
}

// TestAutomationFeaturePresence_DecisionWait: a decision_wait member holds only
// its own branch for an operator answer, asks one question with two options
// that both continue, and reads as waiting, never blocked.
func TestAutomationFeaturePresence_DecisionWait(t *testing.T) {
	a := &Automation{
		ID:        "presence-decision",
		Name:      "decide",
		CreatedAt: time.Now().Add(-time.Hour),
		Members: []AutomationMember{
			{ID: "d1", Kind: MemberKindDecisionWait, Question: "Ship the change?",
				Status: MemberRunning, WaitingForOperator: true},
			{ID: "d2", Kind: MemberKindDecisionWait, Question: "Second?", Status: MemberCompleted,
				WaitingForOperator: true},
		},
	}

	if !memberWaitingForOperator(&a.Members[0]) {
		t.Fatal("a running decision_wait member awaiting an answer must read as waiting for the operator")
	}
	if memberWaitingForOperator(&a.Members[1]) {
		t.Fatal("an ended decision_wait member must not read as waiting, whatever its flag says")
	}
	if DecisionWaitReason != "waiting for you" {
		t.Fatalf("DecisionWaitReason = %q, want %q", DecisionWaitReason, "waiting for you")
	}

	q := buildDecisionWaitQuestion(a, &a.Members[0])
	if q.AskedBy != DecisionWaitQuestionAsker || !q.Blocking || q.Default != DecisionWaitOptionContinue {
		t.Fatalf("question asker/blocking/default = %q/%v/%q, want %q/true/%q",
			q.AskedBy, q.Blocking, q.Default, DecisionWaitQuestionAsker, DecisionWaitOptionContinue)
	}
	if len(q.Options) != 2 || q.Options[0].ID != DecisionWaitOptionContinue || q.Options[1].ID != DecisionWaitOptionContinueNoted {
		t.Fatalf("question options = %+v, want continue and continue_noted", q.Options)
	}
	if q.SourceLineSHA256 != decisionWaitKey(a.ID, "d1") {
		t.Fatal("question dedupe key must be derived from the Automation and the member")
	}
	if decisionWaitKey(a.ID, "d1") == decisionWaitKey(a.ID, "d2") {
		t.Fatal("two members must never share a decision wait dedupe key")
	}
	if !strings.Contains(q.Text, "Ship the change?") {
		t.Fatalf("question text %q must carry the member's own question", q.Text)
	}
	if got := truncateRunes("ééééé", 2); got != "éé" {
		t.Fatalf("truncateRunes cut a multi-byte rune badly: %q", got)
	}
	if decisionWaitSweepGrace <= 0 {
		t.Fatal("the orphan sweep needs a positive grace period for a freshly raised question")
	}
}

// TestAutomationFeaturePresence_MaxParallel: a limit caps the members running at
// once, the narrower of the Automation and fork limits applies, and a member
// that finds its scope full holds its cursor flagged waiting for a slot.
func TestAutomationFeaturePresence_MaxParallel(t *testing.T) {
	a := &Automation{
		ID:          "presence-slots",
		MaxParallel: 2,
		CreatedAt:   time.Now().Add(-time.Hour),
		Members: []AutomationMember{
			{ID: "b1", Kind: MemberKindPRD, PRDID: "p1", Status: MemberRunning},
			{ID: "b2", Kind: MemberKindPRD, PRDID: "p2", Status: MemberPending},
			{ID: "b3", Kind: MemberKindPRD, PRDID: "p3", Status: MemberPending},
			{ID: "b4", Kind: MemberKindPRD, PRDID: "p4", Status: MemberPending},
		},
	}

	fork := &AutomationMember{ID: "f1", Kind: MemberKindFork, MaxParallel: 1}
	if got := effectiveMaxParallel(a, fork); got != 1 {
		t.Fatalf("effectiveMaxParallel with fork limit 1 under automation limit 2 = %d, want 1 (narrower wins)", got)
	}
	if got := effectiveMaxParallel(a, nil); got != 2 {
		t.Fatalf("effectiveMaxParallel outside a fork = %d, want 2", got)
	}
	if got := effectiveMaxParallel(&Automation{}, nil); got != 0 {
		t.Fatalf("no limit anywhere = %d, want 0 (no limit)", got)
	}

	// One member runs and the limit is 2: exactly one of the three pending
	// members is admitted, the first in member order.
	admit := parallelAdmit(a, []string{"b4", "b3", "b2"})
	if !admit["b2"] || admit["b3"] || admit["b4"] {
		t.Fatalf("parallelAdmit = %v, want only b2 admitted (first in member order)", admit)
	}

	a.Members[1].WaitingForSlot = true
	if !memberWaitingForSlot(&a.Members[1]) {
		t.Fatal("an unstarted member flagged WaitingForSlot must read as waiting for a slot")
	}
	a.Members[0].WaitingForSlot = true
	if memberWaitingForSlot(&a.Members[0]) {
		t.Fatal("a running member must never read as waiting for a slot")
	}
	if SlotWaitReason == "" {
		t.Fatal("SlotWaitReason must be set so a held member has a plain reason")
	}
}

// TestAutomationFeaturePresence_ParallelSettings: the operator controls
// set_max_parallel and set_stage_gate store their own field, refuse what they
// must, and record a decision for the change.
func TestAutomationFeaturePresence_ParallelSettings(t *testing.T) {
	h, a := newSlotHarness(t, branchIDs(3), 0, 0, BranchAllMustSucceed)

	updated, err := h.m.SetMaxParallel(a.ID, "", 2, "", 0)
	if err != nil {
		t.Fatalf("SetMaxParallel on the Automation: %v", err)
	}
	if updated.MaxParallel != 2 {
		t.Fatalf("Automation max_parallel = %d after set to 2", updated.MaxParallel)
	}
	if _, ok := lastDecisionOfKind(h.get(a), controlSetMaxParallel); !ok {
		t.Fatal("set_max_parallel must record a decision")
	}

	if _, err := h.m.SetMaxParallel(a.ID, "f1", 1, "", 0); err != nil {
		t.Fatalf("SetMaxParallel on fork f1: %v", err)
	}
	for _, m := range h.get(a).Members {
		if m.ID == "f1" && m.MaxParallel != 1 {
			t.Fatalf("fork f1 max_parallel = %d, want 1", m.MaxParallel)
		}
	}
	if _, err := h.m.SetMaxParallel(a.ID, "b1", 1, "", 0); err == nil {
		t.Fatal("max_parallel on a non-fork member must be refused")
	}
	if _, err := h.m.SetMaxParallel(a.ID, "", -1, "", 0); err == nil {
		t.Fatal("a negative max_parallel must be refused")
	}

	on, err := h.m.SetStageGate(a.ID, true, "", 0)
	if err != nil {
		t.Fatalf("SetStageGate on: %v", err)
	}
	if !on.StageGate {
		t.Fatal("stage_gate must be stored as on")
	}
	if _, ok := lastDecisionOfKind(h.get(a), controlSetStageGate); !ok {
		t.Fatal("set_stage_gate must record a decision")
	}
	off, err := h.m.SetStageGate(a.ID, false, "", 0)
	if err != nil {
		t.Fatalf("SetStageGate off: %v", err)
	}
	if off.StageGate {
		t.Fatal("stage_gate must be stored as off")
	}
}

// TestAutomationFeaturePresence_StageGate: with the gate on, a later stage waits
// for the whole earlier stage; with it off, a stage is only a label.
func TestAutomationFeaturePresence_StageGate(t *testing.T) {
	branches := bs("b1", "build", "r1", "review")

	h, a := newStageHarness(t, true, 0, BranchAllMustSucceed, "", branches...)
	if got := h.stageWaitingIDs(a); !reflect.DeepEqual(got, []string{"r1"}) {
		t.Fatalf("gate on: held members = %v, want [r1] (review waits for build)", got)
	}
	if got := stageOrder(h.get(a)); !reflect.DeepEqual(got, []string{"build", "review"}) {
		t.Fatalf("stageOrder = %v, want [build review] (first appearance)", got)
	}
	if !containsID(h.startedIDs(), "b1") || containsID(h.startedIDs(), "r1") {
		t.Fatalf("gate on: started = %v, want b1 started and r1 held", h.startedIDs())
	}

	h2, a2 := newStageHarness(t, false, 0, BranchAllMustSucceed, "", branches...)
	if got := h2.stageWaitingIDs(a2); len(got) != 0 {
		t.Fatalf("gate off: held members = %v, want none", got)
	}
	if !containsID(h2.startedIDs(), "r1") {
		t.Fatalf("gate off: started = %v, want r1 started with b1 (stage is a label only)", h2.startedIDs())
	}
}
