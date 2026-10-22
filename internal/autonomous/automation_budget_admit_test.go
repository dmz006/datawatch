package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// admitFixture is a Manager with one Automation of one member PRD, a budget
// evaluator on a locked fake clock and a settable spend, and one pending task.
type admitFixture struct {
	t    *testing.T
	m    *Manager
	auto *Automation
	prd  *PRD
	task *Task

	mu    sync.Mutex
	now   time.Time
	spent float64
}

func newAdmitFixture(t *testing.T, p *BudgetPolicy) *admitFixture {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &admitFixture{t: t, m: m, now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	prd := makeAutomationMemberPRDs(t, m, 1)[0]
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}}); err != nil {
		t.Fatal(err)
	}
	a, err := m.CreateAutomation(AutomationInput{
		Name: "gated", Members: []AutomationMember{{ID: "m0", PRDID: prd.ID}}, BudgetPolicy: p,
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	started := f.now.Add(-48 * time.Hour)
	a, err = m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		a.StartedAt = &started
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b := NewAutomationBudget(nil, nil)
	b.Clock = f.clock
	b.Spend = func(*Automation, *AutomationMember, time.Time, string) float64 {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.spent
	}
	m.SetAutomationBudget(b)
	m.SetAutomationBudgetPoll(5 * time.Millisecond)

	f.auto = a
	f.prd, _ = m.Store().GetPRD(prd.ID)
	snap, _ := m.Store().SnapshotPRD(prd.ID)
	tk := snap.Story[0].Tasks[0]
	f.task = &tk
	return f
}

func (f *admitFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *admitFixture) setNow(at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = at
}

func (f *admitFixture) setSpent(usd float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spent = usd
}

func (f *admitFixture) taskNow() Task {
	cur, _ := f.m.Store().TaskSnapshot(f.task.ID)
	return cur
}

// admitAsync runs admit in a goroutine and returns the channel of its result.
func (f *admitFixture) admitAsync(ctx context.Context, role capacity.Role) <-chan error {
	done := make(chan error, 1)
	go func() { done <- f.m.admit(ctx, f.prd, f.task, "claude-code", "m", role) }()
	return done
}

func (f *admitFixture) waitHeld() Task {
	f.t.Helper()
	pollUntil(f.t, 3*time.Second, "task waiting on the automation budget", func() bool {
		return f.taskNow().Status == TaskWaitingCapacity
	})
	return f.taskNow()
}

func expectAdmitted(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admit returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admit did not return")
	}
}

func TestAdmitWaitsWhenAutomationOverCeiling(t *testing.T) {
	f := newAdmitFixture(t, dayPolicy(10))
	f.setSpent(12)

	done := f.admitAsync(context.Background(), capacity.RoleWorker)
	held := f.waitHeld()
	if held.WaitReason == "" {
		t.Fatal("a held task must carry a wait reason")
	}
	select {
	case err := <-done:
		t.Fatalf("admit returned %v while the Automation is over its ceiling", err)
	case <-time.After(50 * time.Millisecond):
	}

	f.setSpent(3)
	expectAdmitted(t, done)
	cur := f.taskNow()
	if cur.Status != TaskInProgress || cur.WaitReason != "" {
		t.Fatalf("after the hold clears the task must run: status=%s reason=%q", cur.Status, cur.WaitReason)
	}
}

func TestAdmitReasonNamesAutomationBudget(t *testing.T) {
	f := newAdmitFixture(t, dayPolicy(10))
	f.setSpent(12.5)
	var resetMu sync.Mutex
	var resetAt *time.Time
	f.m.AutomationBudget().Resets = func(*BudgetPolicy, BudgetWindowSpan, time.Time) ResetInfo {
		resetMu.Lock()
		defer resetMu.Unlock()
		if resetAt == nil {
			return ResetInfo{}
		}
		return ResetInfo{At: resetAt, Known: true, Source: BudgetResetWindow}
	}

	done := f.admitAsync(context.Background(), capacity.RoleWorker)
	reason := f.waitHeld().WaitReason
	for _, want := range []string{"automation budget reached", "automation", "$12.50", "$10.00", "reset time unknown"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q should contain %q", reason, want)
		}
	}
	if strings.Contains(reason, "BL") {
		t.Errorf("reason %q must not carry backlog identifiers", reason)
	}

	// A known reset time is named instead of "unknown".
	at := f.clock().Add(6 * time.Hour)
	resetMu.Lock()
	resetAt = &at
	resetMu.Unlock()
	pollUntil(t, 3*time.Second, "reason updated with the reset time", func() bool {
		r := f.taskNow().WaitReason
		return strings.Contains(r, "resets 2026-10-22 18:00 UTC") && !strings.Contains(r, "unknown")
	})

	f.setSpent(0)
	expectAdmitted(t, done)
}

func TestAdmitResumesAfterWindowRollover(t *testing.T) {
	f := newAdmitFixture(t, dayPolicy(10))
	// Spend counts only inside the current window, like the real rollup.
	windowEnd := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	f.m.AutomationBudget().Spend = func(_ *Automation, _ *AutomationMember, since time.Time, _ string) float64 {
		if since.Before(windowEnd) {
			return 12
		}
		return 0
	}

	done := f.admitAsync(context.Background(), capacity.RoleWorker)
	f.waitHeld()

	f.setNow(windowEnd.Add(5 * time.Minute))
	expectAdmitted(t, done)
	if cur := f.taskNow(); cur.Status != TaskInProgress || cur.WaitReason != "" || cur.RetryCount != 0 {
		t.Fatalf("task after rollover: %+v", cur)
	}
}

func TestAdmitLeavesInProgressTasksAlone(t *testing.T) {
	f := newAdmitFixture(t, dayPolicy(10))
	f.setSpent(12)
	f.m.Store().MutateTask(f.task.ID, func(c *Task) { c.Status = TaskInProgress })

	// A verifier admission for the running task is not held, and does not
	// touch its status.
	expectAdmitted(t, f.admitAsync(context.Background(), capacity.RoleVerifier))
	if cur := f.taskNow(); cur.Status != TaskInProgress || cur.WaitReason != "" {
		t.Fatalf("running task disturbed: %+v", cur)
	}
	// Planner work is not held either.
	expectAdmitted(t, f.admitAsync(context.Background(), capacity.RolePlanner))
	// The hold itself still reports the breach, so only the role decides.
	if hold, _ := f.m.automationBudgetHold(f.prd); !hold {
		t.Fatal("the Automation is over its ceiling and should hold new starts")
	}
}

func TestAdmitIgnoresStandaloneAutomaton(t *testing.T) {
	f := newAdmitFixture(t, dayPolicy(10))
	f.setSpent(1000)
	standalone := makeAutomationMemberPRDs(t, f.m, 1)[0]
	if standalone.AutomationID != "" {
		t.Fatalf("standalone PRD has automation %q", standalone.AutomationID)
	}
	if hold, reason := f.m.automationBudgetHold(standalone); hold || reason != "" {
		t.Fatalf("standalone automaton held: %q", reason)
	}
	tk := &Task{ID: "standalone-task", PRDID: standalone.ID}
	done := make(chan error, 1)
	go func() {
		done <- f.m.admit(context.Background(), standalone, tk, "claude-code", "m", capacity.RoleWorker)
	}()
	expectAdmitted(t, done)

	// No evaluator, a missing Automation or a non-member PRD hold nothing.
	f.m.SetAutomationBudget(nil)
	if hold, _ := f.m.automationBudgetHold(f.prd); hold {
		t.Fatal("held with no evaluator wired")
	}
	ghost := *f.prd
	ghost.AutomationID = "no-such-automation"
	f.m.SetAutomationBudget(NewAutomationBudget(nil, nil))
	if hold, _ := f.m.automationBudgetHold(&ghost); hold {
		t.Fatal("held for a missing Automation")
	}
	stranger := *standalone
	stranger.AutomationID = f.auto.ID
	if hold, _ := f.m.automationBudgetHold(&stranger); hold {
		t.Fatal("held a PRD that is not a member")
	}
}

func TestAdmitPacingDoesNotHoldRunningMemberTasks(t *testing.T) {
	p := dayPolicy(10)
	p.Pacing = "even"
	f := newAdmitFixture(t, p)
	// Noon is half of the day window: $5 allowed so far, $8 spent. That is
	// ahead of pace but under the ceiling, so only the advance gate cares.
	f.setSpent(8)
	if v := f.m.AutomationBudget().Evaluate(f.auto, &f.auto.Members[0], false); !v.Gate || v.Kind != BudgetKindPacing {
		t.Fatalf("setup: soft verdict = %+v, want a pacing gate", v)
	}
	if hold, _ := f.m.automationBudgetHold(f.prd); hold {
		t.Fatal("pacing must not hold a member's task")
	}
	expectAdmitted(t, f.admitAsync(context.Background(), capacity.RoleWorker))
	if cur := f.taskNow(); cur.Status == TaskWaitingCapacity {
		t.Fatalf("task held by pacing: %+v", cur)
	}
}

func TestAdmitCancelWhileWaitingForAutomationBudget(t *testing.T) {
	f := newAdmitFixture(t, dayPolicy(10))
	f.setSpent(12)

	done := f.admitAsync(context.Background(), capacity.RoleWorker)
	f.waitHeld()
	if _, err := f.m.CancelTask(f.prd.ID, f.task.ID, "operator", "test"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != errCancelledWhileWaiting {
			t.Fatalf("admit returned %v, want errCancelledWhileWaiting", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admit did not return after the task was cancelled")
	}
	if cur := f.taskNow(); cur.Status != TaskCancelled {
		t.Fatalf("cancelled task must stay cancelled, got %s", cur.Status)
	}
}

func TestAdmitBudgetWaitIsNotAFailure(t *testing.T) {
	f := newAdmitFixture(t, dayPolicy(10))
	f.setSpent(12)

	ctx, cancel := context.WithCancel(context.Background())
	done := f.admitAsync(ctx, capacity.RoleWorker)
	held := f.waitHeld()
	if held.Status != TaskWaitingCapacity || held.RetryCount != 0 {
		t.Fatalf("held task = status %s retries %d, want waiting_capacity and 0", held.Status, held.RetryCount)
	}
	// Waiting never times out: the capacity wait bound does not apply.
	time.Sleep(60 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("admit gave up while waiting: %v", err)
	default:
	}
	if cur := f.taskNow(); cur.Status != TaskWaitingCapacity || cur.RetryCount != 0 {
		t.Fatalf("task after waiting: status %s retries %d", cur.Status, cur.RetryCount)
	}

	// Only context cancellation ends the wait without the budget clearing.
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("admit returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admit ignored context cancellation")
	}
}
