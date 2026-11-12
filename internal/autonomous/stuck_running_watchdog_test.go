package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// stuckWatchdogEnv is a Manager with an injectable clock, a fake session
// lister, a recording refire hook and a recording audit sink and event bus.
type stuckWatchdogEnv struct {
	m       *Manager
	mu      sync.Mutex
	now     time.Time
	sess    []SessionInfo
	live    map[string]bool
	refires []string
	audits  []string
	events  []LifecycleEvent
}

func newStuckWatchdogEnv(t *testing.T, boundSeconds int) *stuckWatchdogEnv {
	t.Helper()
	cfg := DefaultConfig()
	cfg.StuckRunningSeconds = boundSeconds
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	e := &stuckWatchdogEnv{m: m, now: time.Unix(1_700_000_000, 0), live: map[string]bool{}}
	m.orphanNow = func() time.Time {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.now
	}
	m.SetSessionListerFn(func() []SessionInfo {
		e.mu.Lock()
		defer e.mu.Unlock()
		return append([]SessionInfo(nil), e.sess...)
	})
	m.SetStuckRunningHooks(func(id string) (bool, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.refires = append(e.refires, id)
		return true, nil
	}, func(id string) bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.live[id]
	})
	m.SetAuditFn(func(action string, _ map[string]any) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.audits = append(e.audits, action)
	})
	cancel := m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.events = append(e.events, ev)
	})
	t.Cleanup(cancel)
	return e
}

func (e *stuckWatchdogEnv) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

func (e *stuckWatchdogEnv) refireCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.refires)
}

func (e *stuckWatchdogEnv) seen(id string) bool {
	e.m.mu.Lock()
	defer e.m.mu.Unlock()
	_, ok := e.m.stuckSeen[id]
	return ok
}

// waitEvent polls until a status event for the PRD with the new status arrives.
func (e *stuckWatchdogEnv) waitEvent(t *testing.T, prdID string, status PRDStatus) LifecycleEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		for _, ev := range e.events {
			if ev.PRDID == prdID && ev.NewStatus == string(status) {
				e.mu.Unlock()
				return ev
			}
		}
		e.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no status event with new status %s for prd %s", status, prdID)
	return LifecycleEvent{}
}

func TestStuckRunningWatchdog_RefiresOnceThenBlocks(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	prd, _ := bl451RunningPRD(t, e.m, nil, TaskCompleted)
	bound := 60 * time.Second

	e.m.sweepStuckRunning() // first sight
	if !e.seen(prd.ID) {
		t.Fatal("first sweep did not record a first-seen time")
	}
	e.advance(bound)
	e.m.sweepStuckRunning() // exactly the bound: not yet over
	if e.refireCount() != 0 {
		t.Fatalf("refired at the bound, want only after it")
	}

	e.advance(time.Second)
	e.m.sweepStuckRunning()
	p, _ := e.m.Store().GetPRD(prd.ID)
	if e.refireCount() != 1 {
		t.Fatalf("refires = %d, want 1", e.refireCount())
	}
	if !bl451HasDecision(p, "stuck_running_refire") {
		t.Fatal("no stuck_running_refire decision")
	}
	if p.Status != PRDRunning {
		t.Fatalf("status after refire = %s, want running", p.Status)
	}
	if bl451HasDecision(p, "stuck_running_blocked") {
		t.Fatal("blocked decision recorded before the second bound")
	}

	// Timer restarted: nothing happens until a second full bound passes.
	e.advance(bound)
	e.m.sweepStuckRunning()
	if p, _ = e.m.Store().GetPRD(prd.ID); p.Status != PRDRunning || e.refireCount() != 1 {
		t.Fatalf("acted before the second bound: status=%s refires=%d", p.Status, e.refireCount())
	}

	e.advance(time.Second)
	e.m.sweepStuckRunning()
	p, _ = e.m.Store().GetPRD(prd.ID)
	if p.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", p.Status)
	}
	if p.BlockedReason == "" {
		t.Fatal("blocked without a BlockedReason")
	}
	if !bl451HasDecision(p, "stuck_running_blocked") {
		t.Fatal("no stuck_running_blocked decision")
	}
	if e.refireCount() != 1 {
		t.Fatalf("refires = %d after block, want still 1", e.refireCount())
	}
	e.mu.Lock()
	audits := append([]string(nil), e.audits...)
	e.mu.Unlock()
	if len(audits) != 1 || audits[0] != audit.ActionStuckRunningBlocked {
		t.Fatalf("audits = %v, want [%s]", audits, audit.ActionStuckRunningBlocked)
	}
	ev := e.waitEvent(t, prd.ID, PRDBlocked)
	if ev.OldStatus != string(PRDRunning) || ev.Decision == nil || ev.Decision.Kind != "stuck_running_blocked" {
		t.Fatalf("event = %+v, want running->blocked carrying the stuck_running_blocked decision", ev)
	}
	if e.seen(prd.ID) {
		t.Fatal("first-seen entry left after block")
	}
}

func TestStuckRunningWatchdog_LeavesActiveWorkAlone(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	inProgress, _ := bl451RunningPRD(t, e.m, nil, TaskInProgress)
	pending, _ := bl451RunningPRD(t, e.m, nil, TaskPending)
	withSession, _ := bl451RunningPRD(t, e.m, nil, TaskCompleted)
	withGuardrail, _ := bl451RunningPRD(t, e.m, nil, TaskCompleted)
	withExecutor, _ := bl451RunningPRD(t, e.m, nil, TaskCompleted)
	e.sess = []SessionInfo{
		{ID: "a-1111", Name: "autonomous:implement", PRDID: withSession.ID, TaskID: "t1", Alive: true},
		{ID: "a-2222", Name: "autonomous:guardrail:rules-review", PRDID: withGuardrail.ID, Alive: true},
	}
	e.live[withExecutor.ID] = true
	ids := []string{inProgress.ID, pending.ID, withSession.ID, withGuardrail.ID, withExecutor.ID}

	for i := 0; i < 6; i++ {
		e.m.sweepStuckRunning()
		for _, id := range ids {
			if e.seen(id) {
				t.Fatalf("pass %d: first-seen entry for %s, want none", i, id)
			}
		}
		e.advance(10 * time.Minute)
	}
	if e.refireCount() != 0 {
		t.Fatalf("refires = %d, want 0", e.refireCount())
	}
	for _, id := range ids {
		p, _ := e.m.Store().GetPRD(id)
		if p.Status != PRDRunning || bl451HasDecision(p, "stuck_running_refire") || bl451HasDecision(p, "stuck_running_blocked") {
			t.Fatalf("prd %s touched: status=%s decisions=%d", id, p.Status, len(p.Decisions))
		}
	}
}

func TestStuckRunningWatchdog_ConcurrentStatusChangeClearsEntry(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	prd, _ := bl451RunningPRD(t, e.m, nil, TaskCompleted)

	e.m.sweepStuckRunning()
	if !e.seen(prd.ID) {
		t.Fatal("no first-seen entry")
	}
	// A concurrent writer moves the PRD out of running before the bound.
	p, _ := e.m.Store().GetPRD(prd.ID)
	p.Status = PRDCompleted
	if err := e.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	before := len(p.Decisions)

	e.advance(10 * time.Minute)
	e.m.sweepStuckRunning()
	e.advance(10 * time.Minute)
	e.m.sweepStuckRunning()

	if e.seen(prd.ID) {
		t.Fatal("first-seen entry survived the status change")
	}
	p, _ = e.m.Store().GetPRD(prd.ID)
	if p.Status != PRDCompleted || len(p.Decisions) != before {
		t.Fatalf("PRD written: status=%s decisions=%d (was %d)", p.Status, len(p.Decisions), before)
	}
	if e.refireCount() != 0 {
		t.Fatalf("refires = %d, want 0", e.refireCount())
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.audits) != 0 {
		t.Fatalf("audits = %v, want none", e.audits)
	}
}

// stuckMixedPRD makes a running PRD whose tasks carry the given statuses.
func stuckMixedPRD(t *testing.T, m *Manager, statuses ...TaskStatus) *PRD {
	t.Helper()
	prd, _ := bl451RunningPRD(t, m, nil, TaskCompleted)
	prd, _ = m.Store().GetPRD(prd.ID)
	var tasks []Task
	for i, st := range statuses {
		tasks = append(tasks, Task{ID: fmt.Sprintf("mt%d", i), Title: fmt.Sprintf("task-%d", i), Status: st})
	}
	prd.Story[0].Tasks = tasks
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return prd
}

func TestStuckRunningWatchdog_AdvancesCompletedOrCancelled(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	prd := stuckMixedPRD(t, e.m, TaskCompleted, TaskCancelled, TaskCancelled)
	e.m.sweepStuckRunning()
	e.advance(61 * time.Second)
	e.m.sweepStuckRunning()
	p, _ := e.m.Store().GetPRD(prd.ID)
	if e.refireCount() != 1 {
		t.Fatalf("refires = %d, want 1", e.refireCount())
	}
	var note string
	for _, d := range p.Decisions {
		if d.Kind == "stuck_running_cancelled_advance" {
			note = d.Note
			if d.Actor != "autonomous" {
				t.Fatalf("actor = %q", d.Actor)
			}
		}
	}
	if note == "" {
		t.Fatal("no stuck_running_cancelled_advance decision")
	}
	for _, want := range []string{"task-1", "task-2", "2 cancelled"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note %q missing %q", note, want)
		}
	}
	if strings.Contains(note, "PRD") {
		t.Fatalf("note uses forbidden wording: %q", note)
	}
	if bl451HasDecision(p, "stuck_running_refire") {
		t.Fatal("plain refire decision recorded alongside the advance")
	}
	// Not moved within another full bound: falls through to blocked.
	e.advance(61 * time.Second)
	e.m.sweepStuckRunning()
	p, _ = e.m.Store().GetPRD(prd.ID)
	if p.Status != PRDBlocked || !bl451HasDecision(p, "stuck_running_blocked") {
		t.Fatalf("status = %s, want blocked after a second bound", p.Status)
	}
	if e.refireCount() != 1 {
		t.Fatalf("refires = %d, want still 1", e.refireCount())
	}
}

func TestStuckRunningWatchdog_AllCancelledNotAdvanced(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	prd := stuckMixedPRD(t, e.m, TaskCancelled, TaskCancelled)
	e.m.sweepStuckRunning()
	e.advance(61 * time.Second)
	e.m.sweepStuckRunning()
	p, _ := e.m.Store().GetPRD(prd.ID)
	if bl451HasDecision(p, "stuck_running_cancelled_advance") {
		t.Fatal("an all-cancelled Automaton was advanced")
	}
	if !bl451HasDecision(p, "stuck_running_refire") {
		t.Fatal("all-cancelled Automaton lost the plain refire path")
	}
}

func TestStuckRunningWatchdog_FailedTaskKeepsOldPath(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	prd := stuckMixedPRD(t, e.m, TaskCompleted, TaskCancelled, TaskFailed)
	e.m.sweepStuckRunning()
	e.advance(61 * time.Second)
	e.m.sweepStuckRunning()
	p, _ := e.m.Store().GetPRD(prd.ID)
	if bl451HasDecision(p, "stuck_running_cancelled_advance") || !bl451HasDecision(p, "stuck_running_refire") {
		t.Fatalf("failed-task Automaton not on the plain refire path: %+v", p.Decisions)
	}
	e.advance(61 * time.Second)
	e.m.sweepStuckRunning()
	if p, _ = e.m.Store().GetPRD(prd.ID); p.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", p.Status)
	}
}

// The blocked reason names the cancelled tasks, states the new behaviour, and
// stays one line in Automaton wording.
func TestStuckRunningCancelledReason(t *testing.T) {
	r := stuckRunningCancelledReason(90*time.Second, []string{"Fix login", "Add docs"})
	for _, want := range []string{"Fix login", "Add docs", "2 cancelled tasks", "skipped", "completion review", "grace period", "removed while the Automaton is running"} {
		if !strings.Contains(r, want) {
			t.Errorf("reason %q missing %q", r, want)
		}
	}
	if strings.Contains(r, "\n") || strings.Contains(strings.ToUpper(r), "PRD") {
		t.Errorf("reason must be one line without the word PRD: %q", r)
	}
}
