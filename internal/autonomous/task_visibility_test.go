package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// visFixture returns a manager with a capacity ledger and one running
// Automaton holding n tasks (all pending); the PRD's project dir is a temp
// dir so a tree lock key resolves.
func visFixture(t *testing.T, n int) (*Manager, *PRD, *capacity.Ledger) {
	t.Helper()
	m, _, led := capFixture(t, 4)
	prd := newPRDWithTasks(t, m, n)
	prd.ProjectDir = t.TempDir()
	prd.Title = "Visibility PRD"
	prd.Status = PRDRunning
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd, led
}

func visTaskIDs(t *testing.T, m *Manager, prdID string) []string {
	t.Helper()
	cur, ok := m.Store().SnapshotPRD(prdID)
	if !ok {
		t.Fatalf("prd %s missing", prdID)
	}
	var ids []string
	for _, tk := range cur.Story[0].Tasks {
		ids = append(ids, tk.ID)
	}
	return ids
}

func visSetTask(t *testing.T, m *Manager, id string, f func(*Task)) {
	t.Helper()
	if !m.Store().MutateTask(id, f) {
		t.Fatalf("task %s missing", id)
	}
}

func waitViewFor(queue []TaskWaitView, taskID string) *TaskWaitView {
	for i := range queue {
		if queue[i].TaskID == taskID {
			return &queue[i]
		}
	}
	return nil
}

func holdViewFor(holdings []TaskHoldView, taskID string) *TaskHoldView {
	for i := range holdings {
		if holdings[i].TaskID == taskID {
			return &holdings[i]
		}
	}
	return nil
}

func TestTaskVisibility_QueuedTaskCarriesReason(t *testing.T) {
	m, prd, _ := visFixture(t, 4)
	ids := visTaskIDs(t, m, prd.ID)
	// ids[0] unfinished, ids[1] depends on it, ids[2] and ids[3] are ready.
	visSetTask(t, m, ids[0], func(c *Task) { c.Status = TaskInProgress })
	visSetTask(t, m, ids[1], func(c *Task) { c.Status = TaskQueued; c.DependsOn = []string{ids[0]} })
	visSetTask(t, m, ids[2], func(c *Task) { c.Status = TaskQueued })
	visSetTask(t, m, ids[3], func(c *Task) { c.Status = TaskQueued })

	queue, _ := m.TaskVisibility()
	if len(queue) != 3 {
		t.Fatalf("queue has %d entries, want 3: %+v", len(queue), queue)
	}
	dep := waitViewFor(queue, ids[1])
	if dep == nil || dep.Cause != WaitCauseDependencies || !strings.Contains(dep.Reason, "T0") {
		t.Fatalf("dependent task must name its unfinished dependency by title: %+v", dep)
	}
	if dep.PRDID != prd.ID || dep.PRDTitle != "Visibility PRD" || dep.TaskTitle != "T1" || dep.Since.IsZero() {
		t.Errorf("view missing identifying fields: %+v", dep)
	}
	first := waitViewFor(queue, ids[2])
	if first == nil || first.Cause != WaitCauseNextInLine || first.Reason != "next in line to start" {
		t.Fatalf("first ready task must be next in line: %+v", first)
	}
	second := waitViewFor(queue, ids[3])
	if second == nil || second.Cause != WaitCauseNextInLine || !strings.Contains(second.Reason, "1 ready task(s) ahead") {
		t.Fatalf("second ready task must say how many are ahead: %+v", second)
	}

	// Approved but not started: the Automaton itself is what it waits for.
	prd2, _ := m.Store().GetPRD(prd.ID)
	prd2.Status = PRDApproved
	if err := m.Store().SavePRD(prd2); err != nil {
		t.Fatal(err)
	}
	queue, _ = m.TaskVisibility()
	if v := waitViewFor(queue, ids[2]); v == nil || v.Cause != WaitCauseNotStarted || !strings.Contains(v.Reason, "approved but has not been started") {
		t.Fatalf("approved Automaton must say it has not been started: %+v", v)
	}

	// A capacity wait uses the persisted reason, classified.
	reason := PoolFullWaitText("host", 2, 2, []string{"t9", "t8"})
	visSetTask(t, m, ids[3], func(c *Task) { c.Status = TaskWaitingCapacity; c.WaitReason = reason })
	queue, _ = m.TaskVisibility()
	v := waitViewFor(queue, ids[3])
	if v == nil || v.Cause != WaitCausePoolFull || v.Reason != reason || v.Holder != "t9,t8" {
		t.Fatalf("capacity wait must carry its classified reason and holder: %+v", v)
	}

	// The autonomous status carries the same rows.
	st := m.Status()
	if len(st.Queue) != len(queue) || waitViewFor(st.Queue, ids[3]) == nil {
		t.Fatalf("Status().Queue = %+v; want the task visibility queue", st.Queue)
	}
}

func TestTaskVisibility_RunningTaskCarriesHoldings(t *testing.T) {
	m, prd, led := visFixture(t, 2)
	ids := visTaskIDs(t, m, prd.ID)
	visSetTask(t, m, ids[0], func(c *Task) { c.Status = TaskInProgress; c.SessionID = "sess-a" })
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: ids[0], PRDID: prd.ID, Pools: []string{"host", "llm:qwen", "node:n1"}, Node: "n1",
	}, time.Second, nil, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release := func() {}
	m.attachTreeRelease(ids[0], &release)

	_, holdings := m.TaskVisibility()
	h := holdViewFor(holdings, ids[0])
	if h == nil {
		t.Fatalf("running task missing from holdings: %+v", holdings)
	}
	treeKey, _, err := ResolveTreeKey(prd, &Task{ID: ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"host", "llm:qwen", "node:n1", treeKey}
	if strings.Join(h.Pools, " ") != strings.Join(want, " ") {
		t.Errorf("pools = %v; want %v", h.Pools, want)
	}
	if h.Node != "n1" || h.SessionID != "sess-a" || h.Since.IsZero() || h.PRDID != prd.ID || h.TaskTitle != "T0" {
		t.Errorf("hold view fields wrong: %+v", h)
	}
	if st := m.Status(); holdViewFor(st.Holdings, ids[0]) == nil {
		t.Errorf("Status().Holdings = %+v; want the running task", st.Holdings)
	}

	// Parked on an external condition: slot released, tree lock kept.
	m.releaseCapacity(ids[0])
	visSetTask(t, m, ids[0], func(c *Task) {
		c.Status = TaskWaitingCapacity
		c.WaitReason = AwaitExternalWaitReasonFor("CI run #42")
	})
	queue, holdings := m.TaskVisibility()
	if v := waitViewFor(queue, ids[0]); v == nil || v.Cause != WaitCauseAwaitExternal {
		t.Fatalf("parked task must be queued as await_external: %+v", v)
	}
	h = holdViewFor(holdings, ids[0])
	if h == nil || len(h.Pools) != 1 || h.Pools[0] != treeKey {
		t.Fatalf("parked task must hold only the tree lock: %+v", h)
	}
	if !strings.Contains(h.Note, "inference slot is released") || !strings.Contains(h.Note, "tree lock "+treeKey+" is kept") {
		t.Errorf("note must say slot released, tree lock kept: %q", h.Note)
	}

	// Tree lock released while parked: the note says so and nothing is held.
	m.detachTreeRelease(ids[0])
	_, holdings = m.TaskVisibility()
	h = holdViewFor(holdings, ids[0])
	if h == nil || len(h.Pools) != 0 || !strings.Contains(h.Note, "tree lock are released") {
		t.Fatalf("parked task with no tree lock must hold nothing: %+v", h)
	}
}

func TestTaskVisibility_EveryWaitCauseHasText(t *testing.T) {
	now := time.Now()
	resets := now.Add(45 * time.Minute)
	clock := func() time.Time { return now }

	cases := []struct {
		name       string
		cause      WaitCause
		reason     string
		wantHolder string
		wantReset  bool
		wire       func(m *Manager)
	}{
		{
			name: "pool full", cause: WaitCausePoolFull,
			reason:     PoolFullWaitText("llm:qwen", 2, 2, []string{"t1", "t2"}),
			wantHolder: "t1,t2",
		},
		{
			name: "node busy", cause: WaitCauseNodeBusy,
			reason:     NodeBusyWaitText("gpu1", 1, 1, []string{"t7"}),
			wantHolder: "t7",
		},
		{
			name: "back-off with reset time", cause: WaitCauseBackoff,
			reason:    BackoffWaitText(clock, "claude-code", resets, true, ""),
			wantReset: true,
			wire: func(m *Manager) {
				bo := NewBackoff()
				bo.Clock = clock
				bo.SetWithReset("claude-code", resets, "usage limit", "test", true)
				m.SetBackoff(bo)
			},
		},
		{
			name: "back-off without reset time", cause: WaitCauseBackoff,
			reason: BackoffWaitText(clock, "claude-code", resets, false, ""),
			wire: func(m *Manager) {
				bo := NewBackoff()
				bo.Clock = clock
				bo.SetWithReset("claude-code", resets, "usage limit", "test", false)
				m.SetBackoff(bo)
			},
		},
		{
			name: "budget window full", cause: WaitCauseBudget,
			reason:    BudgetWaitText(clock, "qwen", 9, 0, 10, &resets),
			wantReset: true,
			wire: func(m *Manager) {
				m.SetBudgetsStatus(func() []BudgetStatus {
					return []BudgetStatus{{LLM: "qwen", CeilingUSD: 10, SpendUSD: 9, Full: true, RollsOverAt: &resets}}
				})
			},
		},
		{
			name: "global cooldown", cause: WaitCauseGlobalCooldown,
			reason:    GlobalCooldownWaitText(clock, resets),
			wantReset: true,
			wire:      func(m *Manager) { m.SetCooldownUntilFn(func() time.Time { return resets }) },
		},
		{
			name: "await external", cause: WaitCauseAwaitExternal,
			reason: AwaitExternalWaitText("CI run #42"),
		},
		{
			name: "await decision", cause: WaitCauseAwaitDecision,
			reason: AwaitDecisionWaitText("q-20261029-abc"),
		},
		{
			name: "tree lock", cause: WaitCauseTreeLock,
			reason:     TreeLockWaitText("tree:/w/proj", "t5", "p5", now.Unix()),
			wantHolder: "t5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, prd, _ := visFixture(t, 1)
			if tc.wire != nil {
				tc.wire(m)
			}
			id := visTaskIDs(t, m, prd.ID)[0]
			visSetTask(t, m, id, func(c *Task) { c.Status = TaskWaitingCapacity; c.WaitReason = tc.reason })

			queue, _ := m.TaskVisibility()
			v := waitViewFor(queue, id)
			if v == nil {
				t.Fatalf("waiting task missing from queue: %+v", queue)
			}
			if v.Cause != tc.cause {
				t.Errorf("cause = %q; want %q", v.Cause, tc.cause)
			}
			if strings.TrimSpace(v.Reason) == "" || v.Reason != tc.reason {
				t.Errorf("reason = %q; want the plain text %q", v.Reason, tc.reason)
			}
			if v.Holder != tc.wantHolder {
				t.Errorf("holder = %q; want %q", v.Holder, tc.wantHolder)
			}
			switch {
			case tc.wantReset && (v.ResetsAt == nil || !v.ResetsAt.Equal(resets)):
				t.Errorf("resets_at = %v; want %v", v.ResetsAt, resets)
			case !tc.wantReset && v.ResetsAt != nil:
				t.Errorf("resets_at = %v; want none", v.ResetsAt)
			}
		})
	}
}
