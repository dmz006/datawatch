package autonomous

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

// slotStarter is a fake starter that records every start and moves the PRD to
// running like the real API.Run, and counts how many started members are live,
// so a test can read the peak number of members running at once.
type slotStarter struct {
	mu   sync.Mutex
	m    *Manager
	runs []string
	live int
	peak int
}

func (s *slotStarter) Run(id string) error {
	s.mu.Lock()
	s.runs = append(s.runs, id)
	s.live++
	if s.live > s.peak {
		s.peak = s.live
	}
	s.mu.Unlock()
	if p, ok := s.m.store.GetPRD(id); ok {
		s.m.store.WithLock(func() { p.Status = PRDRunning })
	}
	return nil
}

func (s *slotStarter) Resume(id, actor string) (any, error) { return nil, s.Run(id) }

func (s *slotStarter) started() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.runs...)
}

func (s *slotStarter) peakLive() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}

func (s *slotStarter) ended() {
	s.mu.Lock()
	s.live--
	s.mu.Unlock()
}

// slotHarness is a coordinator over a fork of branch members (b1..bn, PRD slots
// 1..n) after an entry member e0 (slot 0), with a join j1 behind them.
type slotHarness struct {
	*coordHarness
	st    *slotStarter
	slots map[string]int // member ID -> PRD slot
}

// newSlotHarness builds the fork graph over the branch members ids, written in
// the order given (that is the member order), with the Automation limit and the
// fork limit set, then ends e0 so the fork fires. The fork lists its branches in
// reverse, so a start order that follows the fork list instead of member order
// would show.
func newSlotHarness(t *testing.T, ids []string, autoLimit, forkLimit int, rule string) (*slotHarness, *Automation) {
	t.Helper()
	h := &slotHarness{coordHarness: newCoordHarness(t, len(ids)+1), slots: map[string]int{}}
	h.c.markReady()
	h.st = &slotStarter{m: h.m}
	h.c.starter = h.st
	members := []AutomationMember{{ID: "e0", PRDID: h.prdID(0), OnComplete: "f1"}}
	rev := make([]string, len(ids))
	for i, id := range ids {
		h.slots[id] = i + 1
		members = append(members, AutomationMember{ID: id, PRDID: h.prdID(i + 1)})
		rev[len(ids)-1-i] = id
	}
	members = append(members,
		AutomationMember{ID: "f1", Kind: MemberKindFork, ForkMembers: rev, MaxParallel: forkLimit},
		AutomationMember{ID: "j1", Kind: MemberKindJoin, JoinMembers: ids, OnBranchFailure: rule},
	)
	a, err := h.m.CreateAutomation(AutomationInput{Name: "slots", EntryMemberID: "e0", Members: members, MaxParallel: &autoLimit}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	a = h.activateEntry(a)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	return h, a
}

func branchIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "b" + string(rune('1'+i))
	}
	return ids
}

// startedIDs maps the PRDs the starter ran back to member IDs, in start order.
func (h *slotHarness) startedIDs() []string {
	byPRD := map[string]string{}
	for id, i := range h.slots {
		byPRD[h.prdID(i)] = id
	}
	var out []string
	for _, p := range h.st.started() {
		out = append(out, byPRD[p])
	}
	return out
}

// runningIDs lists the members the Automation records as running, in member
// order.
func (h *slotHarness) runningIDs(a *Automation) []string {
	var out []string
	for _, m := range h.get(a).Members {
		if m.Status == MemberRunning && startsWork(&m) {
			out = append(out, m.ID)
		}
	}
	return out
}

// end finishes branch member id with status s and checks the running count
// stays under limit (0 = no check).
func (h *slotHarness) end(a *Automation, id string, s PRDStatus, limit int) {
	h.t.Helper()
	h.setStatus(h.slots[id], s)
	h.st.ended()
	h.event(h.slots[id], s)
	if limit > 0 {
		if n := len(h.runningIDs(a)); n > limit {
			h.t.Fatalf("after %s ended, %d members run %v, limit %d", id, n, h.runningIDs(a), limit)
		}
	}
}

func (h *slotHarness) waiting(a *Automation) []string {
	var out []string
	for _, m := range h.get(a).Members {
		if memberWaitingForSlot(&m) {
			out = append(out, m.ID)
		}
	}
	return out
}

// Limit 2 over 5 branches: never more than 2 run at once, the other three hold
// their cursors waiting, and each finish starts the next in member order until
// the join fires.
func TestMaxParallelLimitTwoOverFiveBranches(t *testing.T) {
	ids := branchIDs(5)
	h, a := newSlotHarness(t, ids, 2, 0, BranchAllMustSucceed)

	if got, want := h.startedIDs(), []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("starts after the fork = %v, want only %v", got, want)
	}
	if got, want := h.runningIDs(a), []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after fork = %v, want %v (the first two in member order)", got, want)
	}
	if got, want := h.waiting(a), []string{"b3", "b4", "b5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waiting after fork = %v, want %v", got, want)
	}
	// A waiting member holds its cursor, so the Automation is neither blocked
	// nor idle, and the join keeps waiting.
	got := h.get(a)
	if got.Status != AutomationActive || got.IdleSince != nil {
		t.Fatalf("status=%s idle=%v, want active and not idle", got.Status, got.IdleSince)
	}
	if want := []string{"b1", "b2", "b3", "b4", "b5"}; !reflect.DeepEqual(got.ActiveCursors, want) {
		t.Fatalf("cursors = %v, want every branch held: %v", got.ActiveCursors, want)
	}
	if j := h.member(a, "j1"); j.Status != MemberRunning {
		t.Fatalf("join = %+v, want waiting", j)
	}

	// Ending b1 starts b3, the first waiter; ending b2 starts b4; and so on.
	h.end(a, "b1", PRDCompleted, 2)
	if got, want := h.runningIDs(a), []string{"b2", "b3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after b1 = %v, want %v", got, want)
	}
	h.end(a, "b2", PRDCompleted, 2)
	if got, want := h.runningIDs(a), []string{"b3", "b4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after b2 = %v, want %v", got, want)
	}
	h.end(a, "b3", PRDCompleted, 2)
	if got, want := h.runningIDs(a), []string{"b4", "b5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after b3 = %v, want %v", got, want)
	}
	if w := h.waiting(a); len(w) != 0 {
		t.Fatalf("still waiting after every slot freed: %v", w)
	}
	h.end(a, "b4", PRDCompleted, 2)
	if j := h.member(a, "j1"); j.Status != MemberRunning {
		t.Fatalf("join fired with b5 still running: %+v", j)
	}
	h.end(a, "b5", PRDCompleted, 2)

	if want := []string{"b1", "b2", "b3", "b4", "b5"}; !reflect.DeepEqual(h.startedIDs(), want) {
		t.Fatalf("start order = %v, want %v", h.startedIDs(), want)
	}
	if peak := h.st.peakLive(); peak != 2 {
		t.Fatalf("peak members live at once = %d, want 2", peak)
	}
	if j := h.member(a, "j1"); j.Status != MemberCompleted {
		t.Fatalf("join = %+v, want fired", j)
	}
	if fin := h.get(a); fin.Status != AutomationCompleted {
		t.Fatalf("status = %s, want completed", fin.Status)
	}
}

// Slots are handed out in member order, not in the order of the fork's list or
// of the IDs: members written d, c, b, a start in that order.
func TestMaxParallelStartsInMemberOrder(t *testing.T) {
	ids := []string{"d", "c", "b", "a"}
	h, a := newSlotHarness(t, ids, 1, 0, BranchContinueAnyway)

	if got, want := h.runningIDs(a), []string{"d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after fork = %v, want %v", got, want)
	}
	// Ending a member that is not first still releases the first waiter in order.
	h.end(a, "d", PRDCompleted, 1)
	h.end(a, "c", PRDCompleted, 1)
	if got, want := h.startedIDs(), []string{"d", "c", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start order = %v, want member order %v", got, want)
	}
	h.end(a, "b", PRDCompleted, 1)
	h.end(a, "a", PRDCompleted, 1)
	if h.st.peakLive() != 1 {
		t.Fatalf("peak = %d, want 1", h.st.peakLive())
	}
}

// Unset (and explicit 0) is no limit: every branch starts at once, as before.
func TestMaxParallelDefaultStartsAll(t *testing.T) {
	ids := branchIDs(5)
	h, a := newSlotHarness(t, ids, 0, 0, BranchAllMustSucceed)
	if got := h.runningIDs(a); !reflect.DeepEqual(got, ids) {
		t.Fatalf("running = %v, want all %v", got, ids)
	}
	if w := h.waiting(a); len(w) != 0 {
		t.Fatalf("waiting = %v, want none with no limit", w)
	}
	if got, want := h.get(a).ActiveCursors, ids; !reflect.DeepEqual(got, want) {
		t.Fatalf("cursors = %v, want %v", got, want)
	}
	// The default order is unchanged: sorted branch IDs.
	if got, want := h.startedIDs(), ids; !reflect.DeepEqual(got, want) {
		t.Fatalf("start order = %v, want %v", got, want)
	}
	s := summaryFor(t, h.coordHarness, a.ID)
	if s.MaxParallel != 0 || s.RunningMembers != 0 || len(s.WaitingForSlot) != 0 {
		t.Fatalf("summary reports a limit that is not set: %+v", s)
	}
}

// A fork limit narrower than the Automation limit wins, and the Automation
// limit wins when it is the narrower one.
func TestMaxParallelNarrowerLimitWins(t *testing.T) {
	t.Run("fork narrower", func(t *testing.T) {
		h, a := newSlotHarness(t, branchIDs(5), 4, 2, BranchAllMustSucceed)
		if got, want := h.runningIDs(a), []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("running = %v, want the fork limit 2 to win over the Automation limit 4: %v", got, want)
		}
		for _, id := range branchIDs(5) {
			h.end(a, id, PRDCompleted, 2)
		}
		if peak := h.st.peakLive(); peak != 2 {
			t.Fatalf("peak = %d, want 2", peak)
		}
	})
	t.Run("automation narrower", func(t *testing.T) {
		h, a := newSlotHarness(t, branchIDs(5), 2, 4, BranchAllMustSucceed)
		if got, want := h.runningIDs(a), []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("running = %v, want the Automation limit 2 to win over the fork limit 4: %v", got, want)
		}
		for _, id := range branchIDs(5) {
			h.end(a, id, PRDCompleted, 2)
		}
		if peak := h.st.peakLive(); peak != 2 {
			t.Fatalf("peak = %d, want 2", peak)
		}
	})
	t.Run("fork limit alone", func(t *testing.T) {
		h, a := newSlotHarness(t, branchIDs(4), 0, 3, BranchAllMustSucceed)
		if got, want := h.runningIDs(a), []string{"b1", "b2", "b3"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("running = %v, want %v", got, want)
		}
	})
	a := &Automation{MaxParallel: 4}
	if got := effectiveMaxParallel(a, &AutomationMember{MaxParallel: 2}); got != 2 {
		t.Fatalf("effective limit = %d, want 2", got)
	}
	if got := effectiveMaxParallel(a, &AutomationMember{MaxParallel: 9}); got != 4 {
		t.Fatalf("effective limit = %d, want 4", got)
	}
	if got := effectiveMaxParallel(&Automation{}, &AutomationMember{}); got != 0 {
		t.Fatalf("effective limit = %d, want 0 (no limit)", got)
	}
}

// A finishing member releases the next whatever way it ended: a failed branch
// frees its slot too, and the waiting member starts.
func TestMaxParallelFinishingMemberReleasesNext(t *testing.T) {
	h, a := newSlotHarness(t, branchIDs(3), 1, 0, BranchContinueAnyway)
	if got, want := h.runningIDs(a), []string{"b1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running = %v, want %v", got, want)
	}
	h.end(a, "b1", PRDFailed, 1)
	if got, want := h.runningIDs(a), []string{"b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("running after b1 failed = %v, want b2 released: %v", got, want)
	}
	if m := h.member(a, "b3"); !memberWaitingForSlot(&m) {
		t.Fatalf("b3 = %+v, want still waiting", m)
	}
	h.end(a, "b2", PRDCompleted, 1)
	h.end(a, "b3", PRDCompleted, 1)
	if j := h.member(a, "j1"); j.Status != MemberCompleted {
		t.Fatalf("join = %+v, want fired under continue_anyway", j)
	}
}

// Waiting reads as waiting for a free slot in the status data, never blocked.
func TestMaxParallelStatusReadsWaitingNotBlocked(t *testing.T) {
	h, a := newSlotHarness(t, branchIDs(4), 2, 0, BranchAllMustSucceed)
	s := summaryFor(t, h.coordHarness, a.ID)
	if s.Status != string(AutomationActive) || s.BlockedReason != "" {
		t.Fatalf("summary status=%q blocked=%q, want active and not blocked", s.Status, s.BlockedReason)
	}
	if s.MaxParallel != 2 || s.RunningMembers != 2 {
		t.Fatalf("summary max_parallel=%d running=%d, want 2 and 2", s.MaxParallel, s.RunningMembers)
	}
	if want := []string{"b3", "b4"}; !reflect.DeepEqual(s.WaitingForSlot, want) {
		t.Fatalf("waiting_for_slot = %v, want %v", s.WaitingForSlot, want)
	}
	byID := map[string]ActiveCursorSummary{}
	for _, c := range s.ActiveCursors {
		byID[c.MemberID] = c
	}
	for _, id := range []string{"b3", "b4"} {
		c := byID[id]
		if c.Status != CursorStatusWaitingForSlot || c.WaitReason != "waiting for a free slot" {
			t.Fatalf("cursor %s = %+v, want waiting for a free slot", id, c)
		}
		if strings.Contains(c.Status, "block") || strings.Contains(c.WaitReason, "block") {
			t.Fatalf("cursor %s reads as blocked: %+v", id, c)
		}
	}
	for _, id := range []string{"b1", "b2"} {
		if c := byID[id]; c.Status != string(MemberRunning) || c.WaitReason != "" {
			t.Fatalf("cursor %s = %+v, want running", id, c)
		}
	}
	if !h.hasAudit("automation_waiting_for_slot") {
		t.Fatalf("audits = %v, want automation_waiting_for_slot", h.audits)
	}
	// Once the waiters start the waiting fields clear.
	h.end(a, "b1", PRDCompleted, 2)
	h.end(a, "b2", PRDCompleted, 2)
	s = summaryFor(t, h.coordHarness, a.ID)
	if len(s.WaitingForSlot) != 0 {
		t.Fatalf("waiting_for_slot = %v after the slots freed, want none", s.WaitingForSlot)
	}
	for _, c := range s.ActiveCursors {
		if c.WaitReason != "" {
			t.Fatalf("cursor %+v still reads as waiting", c)
		}
	}
}

// Restart: members waiting for a slot survive the daemon restart through
// reconcile. Nothing starts while the slots are still full, nothing is blocked,
// and a slot that freed while the daemon was down is given to the waiters in
// member order.
func TestMaxParallelReconcileResumesWaitingMembers(t *testing.T) {
	t.Run("slots still full", func(t *testing.T) {
		h, a := newSlotHarness(t, branchIDs(5), 2, 0, BranchAllMustSucceed)
		// The daemon restarts: b1 and b2 still run, the waiters are held.
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		h.c.reconcile()
		if got := rs.started(); len(got) != 0 {
			t.Fatalf("reconcile started %v while the slots are full", got)
		}
		got := h.get(a)
		if got.Status != AutomationActive {
			t.Fatalf("status = %s, want active (waiting is not blocked)", got.Status)
		}
		if w := h.waiting(a); !reflect.DeepEqual(w, []string{"b3", "b4", "b5"}) {
			t.Fatalf("waiting after restart = %v, want b3 b4 b5 kept", w)
		}
		if want := []string{"b1", "b2", "b3", "b4", "b5"}; !reflect.DeepEqual(got.ActiveCursors, want) {
			t.Fatalf("cursors after restart = %v, want %v", got.ActiveCursors, want)
		}
	})

	t.Run("a running member ended while the daemon was down", func(t *testing.T) {
		h, a := newSlotHarness(t, branchIDs(5), 2, 0, BranchAllMustSucceed)
		// b1's PRD completed during the downtime and its event was lost.
		h.setStatus(h.slots["b1"], PRDCompleted)
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		if got, want := rs.started(), []string{h.prdID(h.slots["b3"])}; !reflect.DeepEqual(got, want) {
			t.Fatalf("reconcile started %v, want only the first waiter %v", got, want)
		}
		if got, want := h.runningIDs(a), []string{"b2", "b3"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("running = %v, want %v", got, want)
		}
		h.c.reconcile() // idempotent
		if got := rs.started(); len(got) != 1 {
			t.Fatalf("second pass started again: %v", got)
		}
		if w := h.waiting(a); !reflect.DeepEqual(w, []string{"b4", "b5"}) {
			t.Fatalf("waiting = %v, want b4 b5", w)
		}
	})

	t.Run("the crash fell between a release and the next start", func(t *testing.T) {
		// Members are written d c b a, so the member order and the ID order
		// differ: the waiters must still start in member order.
		h, a := newSlotHarness(t, []string{"d", "c", "b", "a"}, 2, 0, BranchContinueAnyway)
		// Lay down the stored state: the first two ended and released their
		// slots, the waiters still hold their cursors, and nothing runs.
		h.forceState(a, func(a *Automation) {
			for _, id := range []string{"d", "c"} {
				m := automationMember(a, id)
				m.Status = MemberCompleted
				dropCursor(a, id)
			}
		})
		h.setStatus(h.slots["d"], PRDCompleted)
		h.setStatus(h.slots["c"], PRDCompleted)
		if w := h.waiting(a); !reflect.DeepEqual(w, []string{"b", "a"}) {
			t.Fatalf("waiting before restart = %v", w)
		}
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		if got, want := rs.started(), []string{h.prdID(h.slots["b"]), h.prdID(h.slots["a"])}; !reflect.DeepEqual(got, want) {
			t.Fatalf("reconcile started %v, want the waiters in member order %v", got, want)
		}
		if w := h.waiting(a); len(w) != 0 {
			t.Fatalf("still waiting after reconcile: %v", w)
		}
		h.c.reconcile()
		if got := rs.started(); len(got) != 2 {
			t.Fatalf("second pass started again: %v", got)
		}
	})

	t.Run("limit one, one slot freed", func(t *testing.T) {
		h, a := newSlotHarness(t, []string{"d", "c", "b", "a"}, 1, 0, BranchContinueAnyway)
		h.forceState(a, func(a *Automation) {
			automationMember(a, "d").Status = MemberCompleted
			dropCursor(a, "d")
		})
		h.setStatus(h.slots["d"], PRDCompleted)
		rs := &slotStarter{m: h.m}
		h.c.starter = rs
		h.c.reconcile()
		if got, want := rs.started(), []string{h.prdID(h.slots["c"])}; !reflect.DeepEqual(got, want) {
			t.Fatalf("reconcile started %v, want only c (the first waiter in member order): %v", got, want)
		}
		if w := h.waiting(a); !reflect.DeepEqual(w, []string{"b", "a"}) {
			t.Fatalf("waiting = %v, want b a", w)
		}
	})
}

// The flag and the held cursors persist through the store, so a daemon restart
// reloads a waiting member as waiting.
func TestMaxParallelPersistsThroughStore(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{"p1": {ID: "p1"}, "p2": {ID: "p2"}, "p3": {ID: "p3"}}
	s1, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatal(err)
	}
	created, err := s1.Create(&Automation{
		Name:          "limited",
		MaxParallel:   3,
		EntryMemberID: "f1",
		Members: []AutomationMember{
			{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"b1", "b2"}, MaxParallel: 1},
			{ID: "b1", PRDID: "p1"},
			{ID: "b2", PRDID: "p2"},
			{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"b1", "b2"}},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s1.Update(created.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		automationMember(a, "b2").WaitingForSlot = true
		a.ActiveCursors = []string{"b1", "b2"}
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
	if got.MaxParallel != 3 {
		t.Fatalf("Automation max_parallel = %d, want 3", got.MaxParallel)
	}
	if f := automationMember(got, "f1"); f.MaxParallel != 1 {
		t.Fatalf("fork max_parallel = %d, want 1", f.MaxParallel)
	}
	if b := automationMember(got, "b2"); !memberWaitingForSlot(b) {
		t.Fatalf("b2 = %+v, want waiting for a slot after reload", b)
	}
}

// The manager takes max_parallel on create and update; an update that omits it
// keeps the stored value.
func TestMaxParallelManagerCreateUpdate(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prds := makeAutomationMemberPRDs(t, m, 1)
	two := 2
	a, err := m.CreateAutomation(AutomationInput{Name: "m", Members: []AutomationMember{{PRDID: prds[0].ID}}, MaxParallel: &two}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if a.MaxParallel != 2 {
		t.Fatalf("created max_parallel = %d, want 2", a.MaxParallel)
	}
	a, err = m.UpdateAutomation(a.ID, a.Version, AutomationInput{Name: "m"}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if a.MaxParallel != 2 {
		t.Fatalf("update without the field changed max_parallel to %d, want it kept at 2", a.MaxParallel)
	}
	zero := 0
	a, err = m.UpdateAutomation(a.ID, a.Version, AutomationInput{Name: "m", MaxParallel: &zero}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if a.MaxParallel != 0 {
		t.Fatalf("update with 0 left max_parallel at %d, want 0 (no limit)", a.MaxParallel)
	}
}

// Validation: never negative, and a member limit is valid on a fork only.
func TestMaxParallelValidation(t *testing.T) {
	base := func() *Automation {
		return &Automation{
			Name: "v", Status: AutomationDraft, EntryMemberID: "f1",
			Members: []AutomationMember{
				{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"b1", "b2"}},
				{ID: "b1", PRDID: "p1"},
				{ID: "b2", PRDID: "p2"},
				{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"b1", "b2"}},
			},
		}
	}
	if err := validateAutomation(base()); err != nil {
		t.Fatalf("base graph invalid: %v", err)
	}
	cases := []struct {
		name string
		edit func(a *Automation)
		want string // empty: valid
	}{
		{"automation limit", func(a *Automation) { a.MaxParallel = 3 }, ""},
		{"automation zero", func(a *Automation) { a.MaxParallel = 0 }, ""},
		{"automation negative", func(a *Automation) { a.MaxParallel = -1 }, "max_parallel -1 must not be negative"},
		{"fork limit", func(a *Automation) { a.Members[0].MaxParallel = 2 }, ""},
		{"fork negative", func(a *Automation) { a.Members[0].MaxParallel = -2 }, "max_parallel -2 must not be negative"},
		{"prd member limit", func(a *Automation) { a.Members[1].MaxParallel = 1 }, "only valid on kind fork"},
		{"join member limit", func(a *Automation) { a.Members[3].MaxParallel = 1 }, "only valid on kind fork"},
		{"prd member negative", func(a *Automation) { a.Members[1].MaxParallel = -1 }, "must not be negative"},
	}
	for _, tc := range cases {
		a := base()
		tc.edit(a)
		err := validateAutomation(a)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
}
