package autonomous

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// chainGraph is the standard chain graph: e0 (prd 0) -> f1 fork over three
// chain heads -> j1 join over the three tails, then p1 (prd 8).
//
//	chain A: a1 (prd 1) -> a2 (prd 2)
//	chain B: b1 (prd 3) -> b2 (prd 4) -> b3 (prd 5)
//	chain C: c1 (prd 6) -> c2 (prd 7)
//
// The fork lists its heads and the join lists its tails out of order on purpose.
func (h *coordHarness) chainGraph(rule string) []AutomationMember {
	return []AutomationMember{
		{ID: "e0", PRDID: h.prdID(0), OnComplete: "f1"},
		{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"c1", "a1", "b1"}},
		{ID: "a1", PRDID: h.prdID(1), OnComplete: "a2"},
		{ID: "a2", PRDID: h.prdID(2)},
		{ID: "b1", PRDID: h.prdID(3), OnComplete: "b2"},
		{ID: "b2", PRDID: h.prdID(4), OnComplete: "b3"},
		{ID: "b3", PRDID: h.prdID(5)},
		{ID: "c1", PRDID: h.prdID(6), OnComplete: "c2"},
		{ID: "c2", PRDID: h.prdID(7)},
		{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"c2", "a2", "b3"}, OnBranchFailure: rule, OnComplete: "p1"},
		{ID: "p1", PRDID: h.prdID(8)},
	}
}

// chainSlot maps a chain graph member ID to its PRD slot.
var chainSlot = map[string]int{"a1": 1, "a2": 2, "b1": 3, "b2": 4, "b3": 5, "c1": 6, "c2": 7, "p1": 8}

// endMember ends chain member id with the PRD status s.
func (h *coordHarness) endMember(id string, s PRDStatus) {
	h.t.Helper()
	i, ok := chainSlot[id]
	if !ok {
		h.t.Fatalf("no PRD slot for member %s", id)
	}
	h.setStatus(i, s)
	h.event(i, s)
}

// startedMembers returns the chain-graph member IDs whose PRDs the starter was
// asked to run, in start order.
func startedMembers(h *coordHarness, st *branchStarter) []string {
	byPRD := map[string]string{h.prdID(0): "e0"}
	for id, i := range chainSlot {
		byPRD[h.prdID(i)] = id
	}
	var out []string
	for _, p := range st.started() {
		out = append(out, byPRD[p])
	}
	return out
}

// newBranchChainHarness is a harness over the nine PRDs of the chain graph, with the
// coordinator ready: a failed PRD's end is held until it is.
func newBranchChainHarness(t *testing.T) *coordHarness {
	t.Helper()
	h := newCoordHarness(t, 9)
	h.c.markReady()
	return h
}

func chainCursors(h *coordHarness, a *Automation) []string {
	return h.get(a).ActiveCursors
}

func TestBranchChainThreeParallelChainsJoin(t *testing.T) {
	h := newBranchChainHarness(t)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.chainGraph(BranchAllMustSucceed), st))
	h.startFork(a)

	// The fork starts only the chain heads, in sorted order; the rest wait.
	if got, want := startedMembers(h, st), []string{"a1", "b1", "c1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts after fork = %v, want only the heads %v", got, want)
	}
	if got, want := chainCursors(h, a), []string{"a1", "b1", "c1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cursors after fork = %v, want %v", got, want)
	}
	for _, id := range []string{"a2", "b2", "b3", "c2"} {
		if m := h.member(a, id); m.Status != MemberPending && m.Status != "" {
			t.Fatalf("chain member %s started with its head: %+v", id, m)
		}
	}

	// A head finishing moves only its own cursor to the next chain member.
	h.endMember("b1", PRDCompleted)
	if got, want := chainCursors(h, a), []string{"a1", "b2", "c1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cursors after b1 = %v, want only chain B to move: %v", got, want)
	}
	if got, want := startedMembers(h, st), []string{"a1", "b1", "c1", "b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts after b1 = %v, want %v", got, want)
	}
	if m := h.member(a, "b1"); m.Status != MemberCompleted || m.EdgeTaken != edgeOnComplete {
		t.Fatalf("b1 not recorded as completed over on_complete: %+v", m)
	}
	if m := h.member(a, "a1"); m.Status != MemberRunning {
		t.Fatalf("chain A was touched by chain B's move: %+v", m)
	}
	if j := h.member(a, "j1"); j.Status != MemberRunning {
		t.Fatalf("join fired before its tails: %+v", j)
	}

	h.endMember("a1", PRDCompleted)
	h.endMember("c1", PRDCompleted)
	h.endMember("b2", PRDCompleted)
	if got, want := chainCursors(h, a), []string{"a2", "b3", "c2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cursors on the second stage = %v, want %v", got, want)
	}

	// Tails ending one by one: the join waits for the last tail.
	h.endMember("a2", PRDCompleted)
	h.endMember("c2", PRDCompleted)
	if j := h.member(a, "j1"); j.Status != MemberRunning {
		t.Fatalf("join fired with chain B still running: %+v", j)
	}
	if got := startedMembers(h, st); containsID(got, "p1") {
		t.Fatalf("p1 started before the join fired: %v", got)
	}
	h.endMember("b3", PRDCompleted)

	j := h.member(a, "j1")
	if j.Status != MemberCompleted || j.Outcome != "joined" || j.EdgeTaken != edgeOnComplete {
		t.Fatalf("join not recorded as joined: %+v", j)
	}
	got := startedMembers(h, st)
	if want := []string{"a1", "b1", "c1", "b2", "a2", "b3", "c2", "p1"}; len(got) != len(want) || got[len(got)-1] != "p1" || countOf(got, "p1") != 1 {
		t.Fatalf("starts = %v, want p1 exactly once after the last tail", got)
	}
	final := h.get(a)
	if final.Status != AutomationActive || !reflect.DeepEqual(final.ActiveCursors, []string{"p1"}) {
		t.Fatalf("after join: status=%s cursors=%v, want active [p1]", final.Status, final.ActiveCursors)
	}
	for _, c := range forkBranchChains(final) {
		if c.Status != BranchChainCompleted || c.FailedMember != "" {
			t.Fatalf("chain %v not completed: %+v", c.Members, c)
		}
	}
}

func TestBranchChainFailureMidChainFollowsJoinRule(t *testing.T) {
	t.Run("all_must_succeed blocks after every chain ended", func(t *testing.T) {
		h := newBranchChainHarness(t)
		st := newBranchStarter()
		a := h.activateEntry(h.build(h.chainGraph(BranchAllMustSucceed), st))
		h.startFork(a)

		h.endMember("b1", PRDCompleted)
		h.endMember("b2", PRDFailed) // mid-chain failure
		if got := h.get(a); got.Status != AutomationActive {
			t.Fatalf("join decided before the other chains ended: status = %s", got.Status)
		}
		if m := h.member(a, "b2"); m.Status != MemberFailed {
			t.Fatalf("b2 = %+v, want failed", m)
		}
		if m := h.member(a, "b3"); m.Status != MemberSkipped || m.Outcome != "branch-failed" {
			t.Fatalf("b3 behind the failure = %+v, want skipped branch-failed", m)
		}
		if got := startedMembers(h, st); containsID(got, "b3") {
			t.Fatalf("b3 started behind a failed member: %v", got)
		}

		for _, id := range []string{"a1", "a2", "c1", "c2"} {
			h.endMember(id, PRDCompleted)
		}
		got := h.get(a)
		if got.Status != AutomationBlocked {
			t.Fatalf("status = %s, want blocked by the failed chain", got.Status)
		}
		var reason string
		for _, d := range got.Decisions {
			if d.Kind == "blocked" {
				reason = d.Note
			}
		}
		if !strings.Contains(reason, "j1") || !strings.Contains(reason, "b3") {
			t.Fatalf("block reason %q must name join j1 and the failed chain's tail b3", reason)
		}
		if containsID(startedMembers(h, st), "p1") {
			t.Fatalf("p1 started past a blocked join")
		}
		if j := h.member(a, "j1"); j.Status != MemberBlocked || j.Outcome != "not-satisfied" {
			t.Fatalf("join = %+v, want blocked not-satisfied", j)
		}
	})

	t.Run("blocked and cancelled chain members end the branch as failed", func(t *testing.T) {
		for _, s := range []PRDStatus{PRDBlocked, PRDCancelled} {
			h := newBranchChainHarness(t)
			st := newBranchStarter()
			a := h.activateEntry(h.build(h.chainGraph(BranchContinueAnyway), st))
			h.startFork(a)
			h.endMember("c1", s)
			if m := h.member(a, "c2"); m.Status != MemberSkipped {
				t.Fatalf("%s: c2 = %+v, want skipped", s, m)
			}
			if got := h.get(a); got.Status != AutomationActive {
				t.Fatalf("%s: a failed branch ended the Automation: %s", s, got.Status)
			}
		}
	})

	t.Run("any_one_is_enough fires on a surviving chain and cancels the rest", func(t *testing.T) {
		h := newBranchChainHarness(t)
		st := newBranchStarter()
		a := h.activateEntry(h.build(h.chainGraph(BranchAnyOneIsEnough), st))
		h.startFork(a)

		h.endMember("a1", PRDFailed) // chain A is lost
		if got := h.get(a); got.Status != AutomationActive || h.member(a, "j1").Status != MemberRunning {
			t.Fatalf("join decided on one failed chain: status=%s", got.Status)
		}
		h.endMember("c1", PRDCompleted)
		h.endMember("c2", PRDCompleted) // chain C succeeds

		j := h.member(a, "j1")
		if j.Status != MemberCompleted {
			t.Fatalf("join = %+v, want fired on the first successful chain", j)
		}
		if m := h.member(a, "b1"); m.Status != MemberCancelled || m.Outcome != "join-fired" {
			t.Fatalf("running chain head b1 = %+v, want cancelled", m)
		}
		for _, id := range []string{"b2", "b3"} {
			if m := h.member(a, id); m.Status != MemberSkipped || m.Outcome != "join-fired" {
				t.Fatalf("unstarted chain member %s = %+v, want skipped", id, m)
			}
		}
		if got := h.get(a); !reflect.DeepEqual(got.ActiveCursors, []string{"p1"}) {
			t.Fatalf("cursors = %v, want only p1 after the join", got.ActiveCursors)
		}
		// Chain B's PRD ending late is a stale no-op.
		h.endMember("b1", PRDCompleted)
		if got := startedMembers(h, st); countOf(got, "p1") != 1 || containsID(got, "b2") {
			t.Fatalf("late end of a cancelled chain changed the run: %v", got)
		}
	})

	t.Run("continue_anyway fires once every chain is terminal", func(t *testing.T) {
		h := newBranchChainHarness(t)
		st := newBranchStarter()
		a := h.activateEntry(h.build(h.chainGraph(BranchContinueAnyway), st))
		h.startFork(a)

		h.endMember("a1", PRDCompleted)
		h.endMember("a2", PRDFailed)
		h.endMember("b1", PRDFailed)
		if h.member(a, "j1").Status != MemberRunning {
			t.Fatalf("join fired with chain C still running")
		}
		h.endMember("c1", PRDCompleted)
		h.endMember("c2", PRDCompleted)
		if j := h.member(a, "j1"); j.Status != MemberCompleted {
			t.Fatalf("join = %+v, want fired when every chain is terminal", j)
		}
		if got := startedMembers(h, st); countOf(got, "p1") != 1 {
			t.Fatalf("starts = %v, want p1 after the join", got)
		}
	})

	t.Run("a chain member that cannot start fails its branch only", func(t *testing.T) {
		h := newBranchChainHarness(t)
		st := newBranchStarter()
		a := h.activateEntry(h.build(h.chainGraph(BranchContinueAnyway), st))
		h.startFork(a)

		h.setStatus(chainSlot["b2"], PRDDraft) // not startable when b1 finishes
		h.endMember("b1", PRDCompleted)
		if got := h.get(a); got.Status != AutomationActive {
			t.Fatalf("an unstartable chain member blocked the Automation: %s", got.Status)
		}
		if m := h.member(a, "b2"); m.Status != MemberFailed || m.Outcome != "gate-denied" {
			t.Fatalf("b2 = %+v, want failed gate-denied", m)
		}
		if m := h.member(a, "b3"); m.Status != MemberSkipped {
			t.Fatalf("b3 = %+v, want skipped", m)
		}
		if m := h.member(a, "a1"); m.Status != MemberRunning {
			t.Fatalf("a sibling chain was disturbed: %+v", m)
		}
	})

	t.Run("a chain member whose start is refused fails its branch only", func(t *testing.T) {
		h := newBranchChainHarness(t)
		st := newBranchStarter()
		a := h.activateEntry(h.build(h.chainGraph(BranchContinueAnyway), st))
		st.failRun[h.prdID(chainSlot["a2"])] = errors.New("spawn refused")
		h.startFork(a)

		h.endMember("a1", PRDCompleted)
		if m := h.member(a, "a2"); m.Status != MemberFailed || m.Outcome != "start-error" {
			t.Fatalf("a2 = %+v, want failed start-error", m)
		}
		if got := h.get(a); got.Status != AutomationActive {
			t.Fatalf("a refused chain start blocked the Automation: %s", got.Status)
		}
	})
}

func TestBranchChainLegacySingleMemberBranchUnchanged(t *testing.T) {
	h := newCoordHarness(t, 4)
	h.c.markReady()
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, "p1"), st))

	fork := automationMember(a, "f1")
	if got := forkTails(a, fork); !reflect.DeepEqual(got, []string{"b1", "b2"}) {
		t.Fatalf("tails of single-member branches = %v, want the heads", got)
	}
	h.startFork(a)
	if got := st.started(); !reflect.DeepEqual(got, []string{h.prdID(1), h.prdID(2)}) {
		t.Fatalf("starts = %v, want both single-member branches", got)
	}
	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted)
	if h.member(a, "j1").Status != MemberRunning {
		t.Fatalf("join fired on one of two branches")
	}
	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted)
	if j := h.member(a, "j1"); j.Status != MemberCompleted || j.EdgeTaken != edgeOnComplete {
		t.Fatalf("join = %+v, want fired", j)
	}
	if got := st.started(); len(got) != 3 || got[2] != h.prdID(3) {
		t.Fatalf("starts = %v, want p1 after the join", got)
	}
	// A single-member branch records no outcome edge, as before.
	if m := h.member(a, "b1"); m.EdgeTaken != "" || m.Status != MemberCompleted {
		t.Fatalf("single-member tail = %+v, want completed with no edge", m)
	}
	for _, c := range forkBranchChains(h.get(a)) {
		if len(c.Members) != 1 || c.Head != c.Tail {
			t.Fatalf("legacy branch chain = %+v, want head == tail", c)
		}
	}
}

func TestBranchChainValidationRejectsInvalidChains(t *testing.T) {
	// base is a valid chain automation over six members: fork f1 over heads a1
	// and b1; chain A is a1 -> a2, chain B is b1 alone; join j1 names a2 and b1.
	base := func() *Automation {
		return &Automation{
			ID: "a0000001", Name: "chain", Status: AutomationActive, EntryMemberID: "f1",
			Members: []AutomationMember{
				{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"a1", "b1"}},
				{ID: "a1", PRDID: "p1", OnComplete: "a2"},
				{ID: "a2", PRDID: "p2"},
				{ID: "b1", PRDID: "p3"},
				{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"a2", "b1"}},
			},
		}
	}
	if err := validateAutomation(base()); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}

	t.Run("a nested automation member is a valid chain member", func(t *testing.T) {
		a := base()
		memberAt(t, a, "a2").PRDID = ""
		memberAt(t, a, "a2").Kind = MemberKindAutomation
		memberAt(t, a, "a2").AutomationID = "c0000001"
		if err := validateAutomation(a); err != nil {
			t.Fatalf("automation chain member rejected: %v", err)
		}
	})

	cases := []struct {
		name   string
		mutate func(t *testing.T, a *Automation)
		want   string
	}{
		{"member shared by two chains", func(t *testing.T, a *Automation) {
			memberAt(t, a, "b1").OnComplete = "a2"
		}, "is in two chains of fork f1"},
		{"member claimed by another fork's chain", func(t *testing.T, a *Automation) {
			a.Members = append(a.Members,
				AutomationMember{ID: "f2", Kind: MemberKindFork, ForkMembers: []string{"a2", "d1"}},
				AutomationMember{ID: "d1", PRDID: "p4"},
				AutomationMember{ID: "j2", Kind: MemberKindJoin, JoinMembers: []string{"a2", "d1"}},
			)
		}, "already in a chain of fork f1"},
		{"chain leaves its fork/join pair through the join", func(t *testing.T, a *Automation) {
			memberAt(t, a, "a2").OnComplete = "j1"
		}, "names join j1"},
		{"chain member is a fork", func(t *testing.T, a *Automation) {
			a.Members = append(a.Members, AutomationMember{ID: "f2", Kind: MemberKindFork, ForkMembers: []string{"a2", "b1"}})
			memberAt(t, a, "a1").OnComplete = "f2"
		}, "branches must be prd or automation members"},
		{"chain loops", func(t *testing.T, a *Automation) {
			memberAt(t, a, "a2").OnComplete = "a1"
		}, "loops back to member a1"},
		{"chain member carries on_failed", func(t *testing.T, a *Automation) {
			memberAt(t, a, "a1").OnFailed = "b1"
		}, "must not carry outcome edges other than on_complete"},
		{"chain member carries on_blocked", func(t *testing.T, a *Automation) {
			memberAt(t, a, "a2").OnBlocked = "b1"
		}, "must not carry outcome edges other than on_complete"},
		{"join names a mid-chain member, not the tail", func(t *testing.T, a *Automation) {
			memberAt(t, a, "j1").JoinMembers = []string{"a1", "b1"}
		}, "not a branch tail"},
		{"join misses a chain tail", func(t *testing.T, a *Automation) {
			a.Members = append(a.Members, AutomationMember{ID: "b2", PRDID: "p4"})
			memberAt(t, a, "b1").OnComplete = "b2" // b1 -> b2 makes b2 the tail
		}, "not a branch tail"},
		{"join names heads of longer chains", func(t *testing.T, a *Automation) {
			memberAt(t, a, "j1").JoinMembers = []string{"a1", "b1"}
		}, "join j1 names a1"},
		{"outside edge enters a chain part-way", func(t *testing.T, a *Automation) {
			a.Members = append(a.Members, AutomationMember{ID: "x1", PRDID: "p4", OnComplete: "a2"})
			memberAt(t, a, "j1").OnComplete = "x1"
		}, "inside a fork branch chain"},
		{"chain names an unknown member", func(t *testing.T, a *Automation) {
			memberAt(t, a, "a2").OnComplete = "zz"
		}, "unknown member zz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := base()
			tc.mutate(t, a)
			err := validateAutomation(a)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestBranchChainSuccessorsAndPairing(t *testing.T) {
	h := newBranchChainHarness(t)
	a := h.build(h.chainGraph(BranchAllMustSucceed), newBranchStarter())

	succ := automationSuccessors(a)
	if !sameMemberSet(succ["f1"], []string{"a1", "b1", "c1"}) {
		t.Fatalf("fork successors = %v, want the chain heads", succ["f1"])
	}
	if !sameMemberSet(succ["b1"], []string{"b2"}) || !sameMemberSet(succ["b2"], []string{"b3"}) {
		t.Fatalf("chain links = %v %v, want b1->b2->b3", succ["b1"], succ["b2"])
	}
	if !sameMemberSet(succ["b3"], []string{"j1"}) {
		t.Fatalf("tail successors = %v, want the paired join", succ["b3"])
	}
	if found, cycle := automationHasCycle(a); found {
		t.Fatalf("valid chains reported a cycle: %v", cycle)
	}
	if un := automationUnreachable(a); len(un) != 0 {
		t.Fatalf("chain members unreachable: %v", un)
	}
	if join := joinFor(a, automationMember(a, "f1")); join == nil || join.ID != "j1" {
		t.Fatalf("joinFor = %+v, want j1", join)
	}
	for _, id := range []string{"a1", "a2", "b1", "b2", "b3", "c1", "c2"} {
		if join := joinOf(a, id); join == nil || join.ID != "j1" {
			t.Fatalf("joinOf(%s) = %+v, want j1", id, join)
		}
	}
	if joinOf(a, "e0") != nil || joinOf(a, "p1") != nil {
		t.Fatalf("a member outside the branches reported a join")
	}
	if fork := forkForJoin(a, automationMember(a, "j1")); fork == nil || fork.ID != "f1" {
		t.Fatalf("forkForJoin = %+v, want f1", fork)
	}
}

// A chain member's end is read from member statuses, so applying the same end
// twice, or re-reading the join, changes nothing the second time.
func TestBranchChainStepIsReplaySafe(t *testing.T) {
	a := &Automation{
		ID: "a0000001", Name: "chain", Status: AutomationActive, EntryMemberID: "f1",
		ActiveCursors: []string{"a1", "b1"},
		Members: []AutomationMember{
			{ID: "f1", Kind: MemberKindFork, Status: MemberCompleted, ForkMembers: []string{"a1", "b1"}},
			{ID: "a1", PRDID: "p1", Status: MemberRunning, OnComplete: "a2"},
			{ID: "a2", PRDID: "p2", Status: MemberPending},
			{ID: "b1", PRDID: "p3", Status: MemberRunning},
			{ID: "j1", Kind: MemberKindJoin, Status: MemberRunning, JoinMembers: []string{"a2", "b1"}},
		},
	}
	snap := map[string]memberInfo{
		"a1": {prdStatus: PRDCompleted}, "a2": {prdStatus: PRDApproved}, "b1": {prdStatus: PRDRunning},
	}
	done, _ := prdEnd(PRDCompleted)
	in := stepInput{memberID: "a1", end: done, now: time.Unix(1700000000, 0)}

	eff, err := step(a, in, snap)
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	if len(eff.starts) != 1 || eff.starts[0].memberID != "a2" || !eff.starts[0].branch {
		t.Fatalf("starts = %+v, want a2 started as a branch member", eff.starts)
	}
	if !reflect.DeepEqual(a.ActiveCursors, []string{"b1", "a2"}) {
		t.Fatalf("cursors = %v, want b1 untouched and a2 added", a.ActiveCursors)
	}
	if _, err := step(a, in, snap); err != errStaleStep {
		t.Fatalf("replayed end = %v, want errStaleStep", err)
	}

	won, pending, failed := classifyBranches(a, automationMember(a, "j1"))
	if len(won) != 0 || !reflect.DeepEqual(pending, []string{"a2", "b1"}) || len(failed) != 0 {
		t.Fatalf("classify = %v %v %v, want both chains pending", won, pending, failed)
	}
	// Reading twice gives the same answer; a restarted join re-read is stale.
	won2, pending2, failed2 := classifyBranches(a, automationMember(a, "j1"))
	if !reflect.DeepEqual(won, won2) || !reflect.DeepEqual(pending, pending2) || !reflect.DeepEqual(failed, failed2) {
		t.Fatalf("classify is not stable across reads")
	}
	if _, err := stepJoinReeval(a, stepInput{memberID: "j1", reeval: true}, snap); err != errStaleStep {
		t.Fatalf("join re-read with live chains = %v, want errStaleStep", err)
	}

	// The failing member decides the chain outcome even before the tail runs.
	failEnd, _ := prdEnd(PRDFailed)
	if _, err := step(a, stepInput{memberID: "a2", end: failEnd, now: time.Unix(1700000100, 0)}, snap); err != nil {
		t.Fatalf("step a2: %v", err)
	}
	status, failedMember := chainOutcome(a, []string{"a1", "a2"})
	if status != BranchChainFailed || failedMember != "a2" {
		t.Fatalf("chainOutcome = %s %s, want failed a2", status, failedMember)
	}
}

func TestBranchChainRestartRetriesChainMemberStart(t *testing.T) {
	h := newBranchChainHarness(t)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.chainGraph(BranchAllMustSucceed), st))
	h.startFork(a)

	// The daemon died after chain member a2 was marked running and before its
	// PRD was started: the PRD is still approved. Boot reconcile starts it once
	// as a branch member.
	h.forceState(a, func(a *Automation) {
		now := time.Now()
		a1 := automationMember(a, "a1")
		a1.Status, a1.FinishedAt, a1.EdgeTaken = MemberCompleted, &now, edgeOnComplete
		a2 := automationMember(a, "a2")
		a2.Status, a2.StartedAt = MemberRunning, &now
		dropCursor(a, "a1")
		addCursor(a, "a2")
	})
	before := countOf(startedMembers(h, st), "a2")
	h.c.reconcileOne(h.get(a))
	if got := countOf(startedMembers(h, st), "a2"); got != before+1 {
		t.Fatalf("a2 start attempts = %d, want exactly one retry (%d before)", got, before)
	}
}

func TestBranchChainStatusExposesChains(t *testing.T) {
	h := newBranchChainHarness(t)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.chainGraph(BranchAllMustSucceed), st))
	h.startFork(a)
	h.endMember("b1", PRDCompleted)
	h.endMember("c1", PRDFailed)

	var summary *AutomationStatusSummary
	for _, s := range h.m.automationStatusSummaries(time.Now()) {
		if s.ID == a.ID {
			s := s
			summary = &s
		}
	}
	if summary == nil {
		t.Fatalf("automation %s missing from the status summaries", a.ID)
	}
	want := []BranchChain{
		{Fork: "f1", Join: "j1", Head: "a1", Tail: "a2", Members: []string{"a1", "a2"}, Status: BranchChainRunning},
		{Fork: "f1", Join: "j1", Head: "b1", Tail: "b3", Members: []string{"b1", "b2", "b3"}, Status: BranchChainRunning},
		{Fork: "f1", Join: "j1", Head: "c1", Tail: "c2", Members: []string{"c1", "c2"}, Status: BranchChainFailed, FailedMember: "c1"},
	}
	if !reflect.DeepEqual(summary.BranchChains, want) {
		t.Fatalf("branch chains = %+v\nwant %+v", summary.BranchChains, want)
	}
	if summary.JoinWait == nil || summary.JoinWait.JoinMemberID != "j1" ||
		!reflect.DeepEqual(summary.JoinWait.PendingBranches, []string{"a2", "b3"}) ||
		!reflect.DeepEqual(summary.JoinWait.FailedBranches, []string{"c2"}) {
		t.Fatalf("join wait = %+v, want pending tails a2 b3 and failed tail c2", summary.JoinWait)
	}
	cursors := map[string]bool{}
	for _, c := range summary.ActiveCursors {
		cursors[c.MemberID] = true
	}
	if !cursors["a1"] || !cursors["b2"] || len(cursors) != 2 {
		t.Fatalf("active cursors = %+v, want a1 and b2 (each branch's current member)", summary.ActiveCursors)
	}
}

func TestBranchChainStatusOmitsChainsWithoutFork(t *testing.T) {
	h := newCoordHarness(t, 3)
	a := h.makeAuto([][3]string{{"", "", ""}}, 1)
	_ = a
	for _, s := range h.m.automationStatusSummaries(time.Now()) {
		if len(s.BranchChains) != 0 {
			t.Fatalf("linear automation reported branch chains: %+v", s.BranchChains)
		}
	}
}

func containsID(ids []string, want string) bool {
	return countOf(ids, want) > 0
}
