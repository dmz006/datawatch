package autonomous

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// branchStarter is a starter that records PRD runs and nested activations and
// can refuse individual starts, so a test can fail one fork branch at a time.
type branchStarter struct {
	mu           sync.Mutex
	runs         []string
	failRun      map[string]error
	activated    []string
	failActivate map[string]error
}

func newBranchStarter() *branchStarter {
	return &branchStarter{failRun: map[string]error{}, failActivate: map[string]error{}}
}

func (s *branchStarter) Run(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, id)
	return s.failRun[id]
}

func (s *branchStarter) Resume(id, actor string) (any, error) {
	return nil, nil
}

func (s *branchStarter) ActivateAutomation(id, actor string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activated = append(s.activated, id)
	return nil, s.failActivate[id]
}

func (s *branchStarter) started() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.runs...)
}

func (s *branchStarter) activations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.activated...)
}

// prdID returns the PRD behind harness slot i.
func (h *coordHarness) prdID(i int) string {
	return h.prds[i].ID
}

// build creates an Automation over members, entry e0, and swaps the harness
// starter for st.
func (h *coordHarness) build(members []AutomationMember, st *branchStarter) *Automation {
	h.t.Helper()
	h.c.starter = st
	a, err := h.m.CreateAutomation(AutomationInput{Name: "fj", EntryMemberID: "e0", Members: members}, "operator")
	if err != nil {
		h.t.Fatalf("CreateAutomation: %v", err)
	}
	return a
}

// activateEntry makes a active with its entry member running, the state the
// coordinator sees after ActivateAutomation.
func (h *coordHarness) activateEntry(a *Automation) *Automation {
	h.t.Helper()
	out, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		now := time.Now()
		a.Status = AutomationActive
		a.StartedAt = &now
		addCursor(a, a.EntryMemberID)
		entry := automationMember(a, a.EntryMemberID)
		entry.Status = MemberRunning
		entry.StartedAt = &now
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

// forkMembers is the standard graph: e0 (prd 0) -> f1 fork over b1, b2 (prds 1
// and 2) -> j1 join with the given rule. When next is set, j1's on_complete
// names p1 (prd 3). The fork's branch list is written out of order on purpose.
func (h *coordHarness) forkMembers(rule, next string) []AutomationMember {
	members := []AutomationMember{
		{ID: "e0", PRDID: h.prdID(0), OnComplete: "f1"},
		{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"b2", "b1"}},
		{ID: "b1", PRDID: h.prdID(1)},
		{ID: "b2", PRDID: h.prdID(2)},
		{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"b1", "b2"}, OnBranchFailure: rule, OnComplete: next},
	}
	if next != "" {
		members = append(members, AutomationMember{ID: "p1", PRDID: h.prdID(3)})
	}
	return members
}

// startFork completes the entry member, which fires the fork.
func (h *coordHarness) startFork(a *Automation) {
	h.t.Helper()
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
}

func (h *coordHarness) member(a *Automation, id string) AutomationMember {
	h.t.Helper()
	got := h.get(a)
	m := automationMember(got, id)
	if m == nil {
		h.t.Fatalf("member %s missing", id)
	}
	return *m
}

func countOf(ids []string, want string) int {
	n := 0
	for _, id := range ids {
		if id == want {
			n++
		}
	}
	return n
}

// childAutomation creates a draft child Automation over harness slot 4 and
// returns it. The child owns that PRD, so the parent must not name it.
func (h *coordHarness) childAutomation() *Automation {
	h.t.Helper()
	child, err := h.m.CreateAutomation(AutomationInput{Name: "child", EntryMemberID: "c1", Members: []AutomationMember{
		{ID: "c1", PRDID: h.prdID(4)},
	}}, "operator")
	if err != nil {
		h.t.Fatalf("CreateAutomation child: %v", err)
	}
	return child
}

// nestedMembers is e0 (prd 0) -> n1 (nested child) with n1's three outcome
// edges naming x1, y1 and z1 (prds 5, 6, 7).
func (h *coordHarness) nestedMembers(childID string) []AutomationMember {
	return []AutomationMember{
		{ID: "e0", PRDID: h.prdID(0), OnComplete: "n1"},
		{ID: "n1", Kind: MemberKindAutomation, AutomationID: childID, OnComplete: "x1", OnBlocked: "y1", OnFailed: "z1"},
		{ID: "x1", PRDID: h.prdID(5)},
		{ID: "y1", PRDID: h.prdID(6)},
		{ID: "z1", PRDID: h.prdID(7)},
	}
}

// endChild moves the child Automation to status and delivers the
// automation-level event the Manager emits for that transition.
func (h *coordHarness) endChild(child *Automation, status AutomationStatus) {
	h.t.Helper()
	if _, err := h.m.automations.Update(child.ID, 0, func(a *Automation) error {
		a.Status = status
		now := time.Now()
		a.FinishedAt = &now
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	h.c.onMemberTerminal(LifecycleEvent{
		Level:        LevelAutomation,
		Kind:         AutomationStatusKind(status),
		AutomationID: child.ID,
		NewStatus:    string(status),
	})
}

func TestCoordinatorForkStartsBranchesIndependently(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), st))
	h.startFork(a)

	want := []string{h.prdID(1), h.prdID(2)}
	if got := st.started(); !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want branches in sorted member order %v", got, want)
	}
	got := h.get(a)
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
	fork := h.member(a, "f1")
	if fork.Status != MemberCompleted || fork.Outcome != "forked" || fork.FinishedAt == nil {
		t.Fatalf("fork not recorded as forked: %+v", fork)
	}
	join := h.member(a, "j1")
	if join.Status != MemberRunning {
		t.Fatalf("join should be waiting, status = %s", join.Status)
	}
	for _, id := range []string{"b1", "b2"} {
		if m := h.member(a, id); m.Status != MemberRunning || m.StartedAt == nil {
			t.Fatalf("branch %s not running: %+v", id, m)
		}
	}
	if !reflect.DeepEqual(got.ActiveCursors, []string{"b1", "b2"}) {
		t.Fatalf("active cursors = %v, want [b1 b2]", got.ActiveCursors)
	}
	if !h.hasAudit("automation_fork") {
		t.Fatalf("audit = %v, want automation_fork", h.audits)
	}
}

func TestCoordinatorBranchStartFailureDoesNotBlockSiblings(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	st.failRun[h.prdID(1)] = errors.New("spawn refused")
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, ""), st))
	h.startFork(a)

	if got := st.started(); !reflect.DeepEqual(got, []string{h.prdID(1), h.prdID(2)}) {
		t.Fatalf("starts = %v, want both branches attempted", got)
	}
	got := h.get(a)
	if got.Status != AutomationActive {
		t.Fatalf("branch start failure blocked the Automation: status = %s", got.Status)
	}
	if m := h.member(a, "b1"); m.Status != MemberFailed || m.Outcome != "start-error" {
		t.Fatalf("failed branch not recorded: %+v", m)
	}
	if m := h.member(a, "b2"); m.Status != MemberRunning {
		t.Fatalf("sibling branch was rolled back: %+v", m)
	}
	if !reflect.DeepEqual(got.ActiveCursors, []string{"b2"}) {
		t.Fatalf("active cursors = %v, want [b2]", got.ActiveCursors)
	}
	if join := h.member(a, "j1"); join.Status != MemberRunning {
		t.Fatalf("join should keep waiting for the sibling, status = %s", join.Status)
	}
	if h.alertCount("warn|") != 0 {
		t.Fatalf("alerts = %v, want no block alert", h.alerts)
	}
}

func TestCoordinatorJoinAllMustSucceedWaitsThenRoutes(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, "p1"), st))
	h.startFork(a)

	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted)
	if got := st.started(); len(got) != 2 {
		t.Fatalf("join routed before every branch succeeded: starts = %v", got)
	}
	if join := h.member(a, "j1"); join.Status != MemberRunning {
		t.Fatalf("join fired after one of two branches: %+v", join)
	}

	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted)
	want := []string{h.prdID(1), h.prdID(2), h.prdID(3)}
	if got := st.started(); !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want %v (p1 after the join)", got, want)
	}
	join := h.member(a, "j1")
	if join.Status != MemberCompleted || join.Outcome != "joined" || join.EdgeTaken != edgeOnComplete {
		t.Fatalf("join not recorded as joined: %+v", join)
	}
	got := h.get(a)
	if got.Status != AutomationActive || !reflect.DeepEqual(got.ActiveCursors, []string{"p1"}) {
		t.Fatalf("after join: status=%s cursors=%v, want active [p1]", got.Status, got.ActiveCursors)
	}
	if h.member(a, "p1").Status != MemberRunning {
		t.Fatalf("p1 not running after join")
	}
}

func TestCoordinatorJoinAllMustSucceedBlocksOnBranchFailure(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAllMustSucceed, "p1"), st))
	h.startFork(a)

	h.setStatus(1, PRDBlocked)
	h.event(1, PRDBlocked)
	if got := h.get(a); got.Status != AutomationActive {
		t.Fatalf("join blocked before every branch ended: status = %s", got.Status)
	}

	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted)
	got := h.get(a)
	if got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	if p1 := h.member(a, "p1"); p1.StartedAt != nil || len(st.started()) != 2 {
		t.Fatalf("p1 started despite the failed branch: starts = %v", st.started())
	}
	var reason string
	for _, d := range got.Decisions {
		if d.Kind == "blocked" {
			reason = d.Note
		}
	}
	if !strings.Contains(reason, "j1") || !strings.Contains(reason, "b1") {
		t.Fatalf("block reason %q must name join j1 and failed branch b1", reason)
	}
	if h.alertCount("warn|") != 1 || !h.hasAudit("automation_join_blocked") {
		t.Fatalf("alerts=%v audits=%v, want one block alert and automation_join_blocked", h.alerts, h.audits)
	}
}

func TestCoordinatorJoinAnyOneIsEnoughFiresOnFirstSuccess(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAnyOneIsEnough, "p1"), st))
	h.startFork(a)

	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted)

	want := []string{h.prdID(1), h.prdID(2), h.prdID(3)}
	if got := st.started(); !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want %v", got, want)
	}
	if join := h.member(a, "j1"); join.Status != MemberCompleted || join.Outcome != "joined" {
		t.Fatalf("join not fired on first success: %+v", join)
	}
	if b2 := h.member(a, "b2"); b2.Status != MemberCancelled || b2.Outcome != "join-fired" {
		t.Fatalf("still-running branch not marked terminal: %+v", b2)
	}
	got := h.get(a)
	if got.Status != AutomationActive || !reflect.DeepEqual(got.ActiveCursors, []string{"p1"}) {
		t.Fatalf("status=%s cursors=%v, want active [p1]", got.Status, got.ActiveCursors)
	}
}

func TestCoordinatorJoinContinueAnywayFiresWhenAllTerminal(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchContinueAnyway, "p1"), st))
	h.startFork(a)

	h.setStatus(1, PRDBlocked)
	h.event(1, PRDBlocked)
	if got := st.started(); len(got) != 2 {
		t.Fatalf("join fired while a branch was still running: starts = %v", got)
	}

	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted)
	want := []string{h.prdID(1), h.prdID(2), h.prdID(3)}
	if got := st.started(); !reflect.DeepEqual(got, want) {
		t.Fatalf("starts = %v, want %v", got, want)
	}
	if join := h.member(a, "j1"); join.Status != MemberCompleted || join.Outcome != "joined" {
		t.Fatalf("join not fired once all branches ended: %+v", join)
	}
	if got := h.get(a); got.Status != AutomationActive {
		t.Fatalf("continue_anyway with a non-success branch blocked the Automation: %s", got.Status)
	}
}

func TestCoordinatorLateBranchDoesNotReFireJoin(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	a := h.activateEntry(h.build(h.forkMembers(BranchAnyOneIsEnough, "p1"), st))
	h.startFork(a)

	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted)
	fired := h.get(a)

	// The sibling that any_one_is_enough cancelled finishes late.
	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted)

	after := h.get(a)
	if after.Version != fired.Version {
		t.Fatalf("late branch wrote to the Automation: version %d -> %d", fired.Version, after.Version)
	}
	if n := countOf(st.started(), h.prdID(3)); n != 1 {
		t.Fatalf("p1 started %d times, want once", n)
	}
	if n := countOf(h.audits, "automation_join"); n != 1 {
		t.Fatalf("automation_join audited %d times, want once", n)
	}
	if join := h.member(a, "j1"); join.Status != MemberCompleted || join.Outcome != "joined" {
		t.Fatalf("late branch changed the join: %+v", join)
	}
}

func TestCoordinatorNestedMemberActivatesChild(t *testing.T) {
	h := newCoordHarness(t, 8)
	st := newBranchStarter()
	child := h.childAutomation()
	a := h.activateEntry(h.build(h.nestedMembers(child.ID), st))
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	if got := st.activations(); !reflect.DeepEqual(got, []string{child.ID}) {
		t.Fatalf("activations = %v, want [%s]", got, child.ID)
	}
	if got := st.started(); len(got) != 0 {
		t.Fatalf("parent started child PRDs directly: %v", got)
	}
	nested := h.member(a, "n1")
	if nested.Status != MemberRunning || nested.StartedAt == nil {
		t.Fatalf("nested member not running: %+v", nested)
	}
	if got := h.get(a); !reflect.DeepEqual(got.ActiveCursors, []string{"n1"}) {
		t.Fatalf("active cursors = %v, want [n1]", got.ActiveCursors)
	}
	if !h.hasAudit("automation_nested_activate") {
		t.Fatalf("audit = %v, want automation_nested_activate", h.audits)
	}
}

func TestCoordinatorNestedMemberCompletionMapsToOnComplete(t *testing.T) {
	h := newCoordHarness(t, 8)
	st := newBranchStarter()
	child := h.childAutomation()
	a := h.activateEntry(h.build(h.nestedMembers(child.ID), st))
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	h.endChild(child, AutomationCompleted)

	nested := h.member(a, "n1")
	if nested.Status != MemberCompleted || nested.EdgeTaken != edgeOnComplete || nested.Outcome != string(AutomationCompleted) {
		t.Fatalf("nested member after child completed: %+v", nested)
	}
	if got := st.started(); !reflect.DeepEqual(got, []string{h.prdID(5)}) {
		t.Fatalf("starts = %v, want only the on_complete target x1", got)
	}
	if got := h.get(a); !reflect.DeepEqual(got.ActiveCursors, []string{"x1"}) {
		t.Fatalf("active cursors = %v, want [x1]", got.ActiveCursors)
	}
}

func TestCoordinatorNestedMemberBlockedMapsToOnBlocked(t *testing.T) {
	h := newCoordHarness(t, 8)
	st := newBranchStarter()
	child := h.childAutomation()
	a := h.activateEntry(h.build(h.nestedMembers(child.ID), st))
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	h.endChild(child, AutomationBlocked)

	nested := h.member(a, "n1")
	if nested.Status != MemberBlocked || nested.EdgeTaken != edgeOnBlocked || nested.Outcome != string(AutomationBlocked) {
		t.Fatalf("nested member after child blocked: %+v", nested)
	}
	if got := st.started(); !reflect.DeepEqual(got, []string{h.prdID(6)}) {
		t.Fatalf("starts = %v, want only the on_blocked target y1", got)
	}
}

func TestCoordinatorNestedMemberCancelledMapsToOnFailed(t *testing.T) {
	h := newCoordHarness(t, 8)
	st := newBranchStarter()
	child := h.childAutomation()
	a := h.activateEntry(h.build(h.nestedMembers(child.ID), st))
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	h.endChild(child, AutomationCancelled)

	nested := h.member(a, "n1")
	if nested.Status != MemberFailed || nested.EdgeTaken != edgeOnFailed || nested.Outcome != string(AutomationCancelled) {
		t.Fatalf("nested member after child cancelled: %+v", nested)
	}
	if got := st.started(); !reflect.DeepEqual(got, []string{h.prdID(7)}) {
		t.Fatalf("starts = %v, want only the on_failed target z1", got)
	}
}

func TestCoordinatorStaleCASSkipsMovedState(t *testing.T) {
	h := newCoordHarness(t, 4)
	st := newBranchStarter()
	members := []AutomationMember{
		{ID: "e0", PRDID: h.prdID(0), OnComplete: "p1"},
		{ID: "p1", PRDID: h.prdID(3)},
	}
	a := h.activateEntry(h.build(members, st))
	h.setStatus(0, PRDCompleted)

	// Between this step's read and its write, the same event lands first and
	// advances the cursor. The outer write must lose the version check, re-read,
	// see e0 is no longer the cursor, and do nothing.
	h.c.preApply = func() {
		h.c.preApply = nil
		h.event(0, PRDCompleted)
	}
	before := h.get(a)
	h.event(0, PRDCompleted)

	got := h.get(a)
	if got.Version != before.Version+1 {
		t.Fatalf("version %d -> %d, want exactly one write", before.Version, got.Version)
	}
	if runs := st.started(); !reflect.DeepEqual(runs, []string{h.prdID(3)}) {
		t.Fatalf("starts = %v, want p1 started once", runs)
	}
	if got.CurrentMemberID != "p1" || h.member(a, "p1").Status != MemberRunning {
		t.Fatalf("cursor=%s p1=%+v, want the first advance's result", got.CurrentMemberID, h.member(a, "p1"))
	}
	if n := countOf(h.audits, "automation_advance"); n != 1 {
		t.Fatalf("automation_advance audited %d times, want once", n)
	}
}
