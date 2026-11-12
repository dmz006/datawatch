package autonomous

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

var errChildDown = errors.New("child down")

// auditCount reports how many audit records have action (coordHarness has
// hasAudit; this adds the count form used in kind-dispatch assertions).
func (h *coordHarness) auditCount(action string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, a := range h.audits {
		if a == action {
			n++
		}
	}
	return n
}

// buildEntry creates a draft Automation over members with the given entry
// member (which may be any kind) and, when st is non-nil, swaps the harness
// starter.
func (h *coordHarness) buildEntry(entry string, members []AutomationMember, st *branchStarter) *Automation {
	h.t.Helper()
	if st != nil {
		h.c.starter = st
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "entry kind", EntryMemberID: entry, Members: members}, "operator")
	if err != nil {
		h.t.Fatalf("CreateAutomation: %v", err)
	}
	return a
}

// forkEntryMembers is a fork over two PRD branches (bravo, charlie) with its
// paired join. The branch list is written out of order on purpose.
func (h *coordHarness) forkEntryMembers() []AutomationMember {
	return []AutomationMember{
		{ID: "f0", Kind: MemberKindFork, ForkMembers: []string{"b2", "b1"}},
		{ID: "b1", PRDID: h.prdID(1)},
		{ID: "b2", PRDID: h.prdID(2)},
		{ID: "j0", Kind: MemberKindJoin, JoinMembers: []string{"b1", "b2"}},
	}
}

func decisionNote(ds []Decision, kind string) string {
	for _, d := range ds {
		if d.Kind == kind {
			return d.Note
		}
	}
	return ""
}

// A PRD entry member starts through its own gate: an approved PRD runs.
func TestAutomationCoordinator_ActivatePRDEntryRunsApproved(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.Start()
	defer h.c.Stop()
	a := h.buildEntry("m0", []AutomationMember{{ID: "m0", PRDID: h.prdID(0)}}, nil)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationActive || got.StartedAt == nil {
		t.Fatalf("status=%s started=%v, want active with a start time", got.Status, got.StartedAt)
	}
	if m := h.member(a, "m0"); m.Status != MemberRunning || m.StartedAt == nil {
		t.Fatalf("entry member = %+v, want running", m)
	}
	runs, resumes := h.fake.counts()
	if len(runs) != 1 || runs[0] != h.prdID(0) || len(resumes) != 0 {
		t.Fatalf("runs=%v resumes=%v, want [%s] once", runs, resumes, h.prdID(0))
	}
	if !h.hasAudit("automation_activate") || !hasCoordDecision(got.Decisions, "activate") {
		t.Fatalf("audits=%v decisions=%+v", h.audits, got.Decisions)
	}
	if n := h.alertCount(""); n != 0 {
		t.Fatalf("alerts = %v, want none", h.alerts)
	}
}

// A paused PRD entry member resumes instead of running.
func TestAutomationCoordinator_ActivatePRDEntryResumesPaused(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.Start()
	defer h.c.Stop()
	h.setStatus(0, PRDPaused)
	a := h.buildEntry("m0", []AutomationMember{{ID: "m0", PRDID: h.prdID(0)}}, nil)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
	runs, resumes := h.fake.counts()
	if len(runs) != 0 || len(resumes) != 1 || resumes[0] != h.prdID(0)+":"+coordinatorActor {
		t.Fatalf("runs=%v resumes=%v, want resume of %s", runs, resumes, h.prdID(0))
	}
}

// A PRD entry member that is not startable blocks the Automation and names
// the PRD in the reason.
func TestAutomationCoordinator_ActivatePRDEntryRefusalNamesPRD(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.Start()
	defer h.c.Stop()
	h.setStatus(0, PRDNeedsReview)
	a := h.buildEntry("m0", []AutomationMember{{ID: "m0", PRDID: h.prdID(0)}}, nil)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	if runs, resumes := h.fake.counts(); len(runs)+len(resumes) != 0 {
		t.Fatalf("started members: runs=%v resumes=%v, want none", runs, resumes)
	}
	if note := decisionNote(got.Decisions, "blocked"); !strings.Contains(note, "alpha") {
		t.Fatalf("blocked decision %q does not name the entry PRD alpha", note)
	}
	if n := h.alertCount("warn|"); n != 1 {
		t.Fatalf("warn alerts = %d (alerts %v), want exactly one", n, h.alerts)
	}
	found := false
	for _, al := range h.alerts {
		if strings.Contains(al, "alpha is needs") {
			found = true
		}
	}
	if !found {
		t.Fatalf("alerts %v never name why alpha cannot start", h.alerts)
	}
}

// A fork entry member fires its branches in sorted order through their own
// gates and starts the paired join.
func TestAutomationCoordinator_ActivateForkEntryFiresBranches(t *testing.T) {
	h := newCoordHarness(t, 4)
	h.c.Start()
	defer h.c.Stop()
	st := newBranchStarter()
	a := h.buildEntry("f0", h.forkEntryMembers(), st)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
	if fork := h.member(a, "f0"); fork.Status != MemberCompleted || fork.Outcome != "forked" {
		t.Fatalf("fork = %+v, want completed/forked", fork)
	}
	for _, id := range []string{"b1", "b2"} {
		if b := h.member(a, id); b.Status != MemberRunning || b.StartedAt == nil {
			t.Fatalf("%s = %+v, want running", id, b)
		}
	}
	if join := h.member(a, "j0"); join.Status != MemberRunning {
		t.Fatalf("join = %+v, want running", join)
	}
	want := []string{h.prdID(1), h.prdID(2)}
	if gotStarts := st.started(); !reflect.DeepEqual(gotStarts, want) {
		t.Fatalf("starts = %v, want %v (sorted branch order)", gotStarts, want)
	}
	if got := h.get(a); !reflect.DeepEqual(got.ActiveCursors, []string{"b1", "b2"}) {
		t.Fatalf("active cursors = %v, want [b1 b2]", got.ActiveCursors)
	}
	if n := h.auditCount("automation_fork"); n != 3 {
		t.Fatalf("automation_fork audited %d times, want the fork fire plus one per branch", n)
	}
	if !h.hasAudit("automation_activate") {
		t.Fatalf("audits=%v, want automation_activate", h.audits)
	}
	if n := h.alertCount(""); n != 0 {
		t.Fatalf("alerts = %v, want none", h.alerts)
	}
}

// A branch the gate refuses fails in place without stopping its siblings.
func TestAutomationCoordinator_ActivateForkEntryGateDeniesOneBranch(t *testing.T) {
	h := newCoordHarness(t, 4)
	h.c.Start()
	defer h.c.Stop()
	h.setStatus(1, PRDNeedsReview)
	st := newBranchStarter()
	a := h.buildEntry("f0", h.forkEntryMembers(), st)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	// The Automation keeps running: one branch still has to finish.
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, want active (a branch is still pending)", got.Status)
	}
	if b := h.member(a, "b1"); b.Status != MemberFailed || b.Outcome != "gate-denied" {
		t.Fatalf("b1 = %+v, want failed with gate-denied", b)
	}
	if b := h.member(a, "b2"); b.Status != MemberRunning {
		t.Fatalf("b2 = %+v, want running", b)
	}
	if join := h.member(a, "j0"); join.Status != MemberRunning {
		t.Fatalf("join = %+v, want running", join)
	}
	if gotStarts := st.started(); !reflect.DeepEqual(gotStarts, []string{h.prdID(2)}) {
		t.Fatalf("starts = %v, want only the startable branch", gotStarts)
	}
	if note := decisionNote(got.Decisions, "branch"); !strings.Contains(note, "bravo") {
		t.Fatalf("branch decision %q does not name the denied PRD bravo", note)
	}
}

// A nested-Automation entry member activates its child through the nested
// starter.
func TestAutomationCoordinator_ActivateNestedEntryActivatesChild(t *testing.T) {
	h := newCoordHarness(t, 6)
	h.c.Start()
	defer h.c.Stop()
	st := newBranchStarter()
	child := h.childAutomation()
	a := h.buildEntry("n0", []AutomationMember{
		{ID: "n0", Kind: MemberKindAutomation, AutomationID: child.ID, OnComplete: "x1"},
		{ID: "x1", PRDID: h.prdID(5)},
	}, st)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
	if active := st.activations(); !reflect.DeepEqual(active, []string{child.ID}) {
		t.Fatalf("activations = %v, want [%s]", active, child.ID)
	}
	if starts := st.started(); len(starts) != 0 {
		t.Fatalf("parent started PRDs directly: %v", starts)
	}
	if n := h.member(a, "n0"); n.Status != MemberRunning || n.StartedAt == nil {
		t.Fatalf("nested entry = %+v, want running", n)
	}
	if got := h.get(a); !reflect.DeepEqual(got.ActiveCursors, []string{"n0"}) {
		t.Fatalf("active cursors = %v, want [n0]", got.ActiveCursors)
	}
	if n := h.auditCount("automation_nested_activate"); n != 1 {
		t.Fatalf("automation_nested_activate audited %d times, want once", n)
	}
	if n := h.alertCount(""); n != 0 {
		t.Fatalf("alerts = %v, want none", h.alerts)
	}
}

// A nested-Automation entry that cannot be activated blocks the parent.
func TestAutomationCoordinator_ActivateNestedEntryStartFails(t *testing.T) {
	h := newCoordHarness(t, 6)
	h.c.Start()
	defer h.c.Stop()
	st := newBranchStarter()
	child := h.childAutomation()
	st.failActivate[child.ID] = errChildDown
	a := h.buildEntry("n0", []AutomationMember{
		{ID: "n0", Kind: MemberKindAutomation, AutomationID: child.ID, OnComplete: "x1"},
		{ID: "x1", PRDID: h.prdID(5)},
	}, st)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	if n := h.alertCount("warn|"); n != 1 {
		t.Fatalf("warn alerts = %d (alerts %v), want one", n, h.alerts)
	}
	if note := decisionNote(got.Decisions, "blocked"); !strings.Contains(note, "child down") {
		t.Fatalf("blocked decision %q does not carry the start failure", note)
	}
}

// The nested release arc: activating a nested entry activates the child, and
// the child's completion takes the entry's on_complete edge.
func TestAutomationCoordinator_ActivateNestedEntryCompletionMapsToOnComplete(t *testing.T) {
	h := newCoordHarness(t, 6)
	h.c.Start()
	defer h.c.Stop()
	st := newBranchStarter()
	child := h.childAutomation()
	a := h.buildEntry("n0", []AutomationMember{
		{ID: "n0", Kind: MemberKindAutomation, AutomationID: child.ID, OnComplete: "x1"},
		{ID: "x1", PRDID: h.prdID(5)},
	}, st)

	if _, err := h.m.ActivateAutomation(a.ID, "alice", 0); err != nil {
		t.Fatal(err)
	}
	h.endChild(child, AutomationCompleted)

	if n := h.member(a, "n0"); n.Status != MemberCompleted || n.EdgeTaken != edgeOnComplete || n.Outcome != string(AutomationCompleted) {
		t.Fatalf("nested entry after child completed: %+v", n)
	}
	if starts := st.started(); !reflect.DeepEqual(starts, []string{h.prdID(5)}) {
		t.Fatalf("starts = %v, want only the on_complete target", starts)
	}
	if got := h.get(a); !reflect.DeepEqual(got.ActiveCursors, []string{"x1"}) {
		t.Fatalf("active cursors = %v, want [x1]", got.ActiveCursors)
	}
}
