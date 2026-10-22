package autonomous

// Back-off admission — task and decompose admission wait in the capacity
// queue while the task's backend is backed off, and a spawn refused by the
// BL30 global cooldown (session.ErrGlobalCooldown) is a wait, not a failure.
// Every test runs on one injectable clock, so nothing depends on wall time.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// stepClock is a manually advanced capacity.Clock whose After channels fire
// when Advance reaches their deadline.
type stepClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []stepTimer
}

type stepTimer struct {
	at time.Time
	ch chan time.Time
}

func newStepClock() *stepClock {
	return &stepClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.pending = append(c.pending, stepTimer{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.pending[:0]
	for _, t := range c.pending {
		if !t.at.After(c.now) {
			t.ch <- c.now
			continue
		}
		kept = append(kept, t)
	}
	c.pending = kept
}

func (c *stepClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// waitTimers blocks until the clock holds at least n unfired timers, so a
// test never advances time before the goroutine under test is waiting.
func (c *stepClock) waitTimers(t *testing.T, n int) {
	t.Helper()
	pollUntil(t, 2*time.Second, fmt.Sprintf("%d pending timers", n), func() bool { return c.Pending() >= n })
}

// backoffFixture wires a manager to a ledger on the step clock, with the
// same llm: gating cmd/datawatch installs: a pool "llm:<backend>" is blocked
// while the shared registry holds a flag for <backend>.
func backoffFixture(t *testing.T) (*Manager, *Backoff, *stepClock, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	clk := newStepClock()
	bo := NewBackoff()
	bo.Clock = clk.Now
	led := capacity.New(capacity.Options{
		PollInterval: time.Second,
		Clock:        clk,
		AutonomousBackpressure: func(pool string) (bool, string) {
			backend, ok := strings.CutPrefix(pool, "llm:")
			if !ok {
				return false, ""
			}
			if blocked, why := bo.Blocked(backend); blocked {
				return true, "backoff: " + why
			}
			return false, ""
		},
	})
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:" + backend}, ""
	})
	m.SetWaitClock(clk)
	return m, bo, clk, led
}

func taskSnapshot(t *testing.T, m *Manager, id string) Task {
	t.Helper()
	tk, ok := m.Store().TaskSnapshot(id)
	if !ok {
		t.Fatalf("task %s missing", id)
	}
	return tk
}

func TestBackoffAdmission_TaskWaitsThenPassesAfterUntil(t *testing.T) {
	m, bo, clk, _ := backoffFixture(t)
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	blockedTask, otherTask := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]
	bo.Set("claude-code", clk.Now().Add(30*time.Second), "rate-limit", BackoffSourceRateLimit)

	// A second backend's pool is unaffected: it is admitted at once.
	if err := m.admit(context.Background(), cur, &otherTask, "pi-prompt", "", capacity.RoleWorker); err != nil {
		t.Fatalf("unaffected backend must be admitted, got %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- m.admit(context.Background(), cur, &blockedTask, "claude-code", "", capacity.RoleWorker)
	}()
	pollUntil(t, 2*time.Second, "task waiting on back-off", func() bool {
		return taskSnapshot(t, m, blockedTask.ID).Status == TaskWaitingCapacity
	})
	if r := taskSnapshot(t, m, blockedTask.ID).WaitReason; !strings.Contains(r, "backoff:") || !strings.Contains(r, "claude-code") {
		t.Fatalf("wait reason should name the back-off, got %q", r)
	}
	select {
	case err := <-done:
		t.Fatalf("admitted while backed off: %v", err)
	default:
	}

	clk.waitTimers(t, 1)
	clk.Advance(31 * time.Second) // past until, and fires the poll tick
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admission after until: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task not admitted after the back-off expired")
	}
	got := taskSnapshot(t, m, blockedTask.ID)
	if got.WaitReason != "" || got.Status == TaskWaitingCapacity || got.RetryCount != 0 {
		t.Fatalf("wait must clear without consuming a retry: %+v", got)
	}
}

func TestBackoffAdmission_WaitTimeoutStillFires(t *testing.T) {
	m, bo, clk, _ := backoffFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 60
	m.mu.Unlock()
	bo.Set("claude-code", clk.Now().Add(time.Hour), "rate-limit", BackoffSourceRateLimit)

	done := make(chan error, 1)
	go func() {
		done <- m.admit(context.Background(), cur, &task, "claude-code", "", capacity.RoleWorker)
	}()
	clk.waitTimers(t, 2) // deadline + poll tick
	clk.Advance(61 * time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, capacity.ErrWaitTimeout) {
			t.Fatalf("want the existing wait-timeout error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait timeout did not fire")
	}
}

func TestBackoffAdmission_DecomposeShowsSameWait(t *testing.T) {
	m, bo, clk, led := backoffFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "claude-code", "", EffortNormal)
	bo.Set("claude-code", clk.Now().Add(30*time.Second), "rate-limit", BackoffSourceRateLimit)
	var calls atomic.Int32
	m.decompose = func(DecomposeRequest) (string, error) {
		calls.Add(1)
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}

	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := m.decomposeAdmitted(prd, "claude-code", "", DecomposeRequest{Spec: "s"})
		done <- result{err}
	}()
	pollUntil(t, 2*time.Second, "planner waiting on back-off", func() bool {
		return strings.Contains(prdWaitReason(t, m, prd.ID), "backoff:")
	})
	if calls.Load() != 0 || !plannerQueued(led, prd.ID) {
		t.Fatalf("planner must be queued, not running (calls=%d)", calls.Load())
	}

	clk.waitTimers(t, 1)
	clk.Advance(31 * time.Second)
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("decompose after until: %v", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("planner not admitted after the back-off expired")
	}
	if calls.Load() != 1 || prdWaitReason(t, m, prd.ID) != "" {
		t.Fatalf("decompose should have run once with the reason cleared (calls=%d)", calls.Load())
	}
}

// cooldownSpawn fails with a wrapped ErrGlobalCooldown while *active is set.
func cooldownSpawn(active *atomic.Bool, calls *atomic.Int32) SpawnFn {
	return func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		calls.Add(1)
		if active.Load() {
			return SpawnResult{}, fmt.Errorf("start session: %w", session.ErrGlobalCooldown)
		}
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
}

func TestBackoffAdmission_GlobalCooldownSpawnWaitsNotFails(t *testing.T) {
	m, _, clk, _ := backoffFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	until := clk.Now().Add(10 * time.Minute)
	m.SetCooldownUntilFn(func() time.Time { return until })

	var active atomic.Bool
	var calls atomic.Int32
	active.Store(true)
	spawn := m.capacityRetrySpawn(&task, cooldownSpawn(&active, &calls))

	type result struct {
		sr  SpawnResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		sr, err := spawn(context.Background(), SpawnRequest{TaskID: task.ID})
		done <- result{sr, err}
	}()
	clk.waitTimers(t, 1)
	got := taskSnapshot(t, m, task.ID)
	if got.Status != TaskWaitingCapacity || !strings.Contains(got.WaitReason, "global cooldown until 12:10") {
		t.Fatalf("task should wait on the cooldown, got status=%s reason=%q", got.Status, got.WaitReason)
	}
	select {
	case r := <-done:
		t.Fatalf("spawn returned while cooldown active: %+v", r)
	default:
	}

	active.Store(false) // cooldown expires
	clk.Advance(capRetryPause)
	select {
	case r := <-done:
		if r.err != nil || r.sr.SessionID == "" {
			t.Fatalf("spawn after cooldown: %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("spawn not retried after the cooldown ended")
	}
	if got := taskSnapshot(t, m, task.ID); got.WaitReason != "" || got.RetryCount != 0 {
		t.Fatalf("wait must clear without consuming a retry: %+v", got)
	}
}

func TestBackoffAdmission_GlobalCooldownSpawnTimesOutLikeCapacity(t *testing.T) {
	m, _, clk, _ := backoffFixture(t)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 60
	m.mu.Unlock()

	var active atomic.Bool
	var calls atomic.Int32
	active.Store(true)
	spawn := m.capacityRetrySpawn(&task, cooldownSpawn(&active, &calls))

	done := make(chan error, 1)
	go func() {
		_, err := spawn(context.Background(), SpawnRequest{TaskID: task.ID})
		done <- err
	}()
	for i := 0; i < 20; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, capacity.ErrWaitTimeout) {
				t.Fatalf("want the capacity wait-timeout error, got %v", err)
			}
			if calls.Load() < 2 {
				t.Fatalf("spawn should have been re-attempted while waiting, calls=%d", calls.Load())
			}
			return
		default:
		}
		clk.waitTimers(t, 1)
		clk.Advance(capRetryPause)
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("cooldown wait never timed out")
}
