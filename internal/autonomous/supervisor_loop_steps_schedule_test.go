package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// schedClock is a clock the test moves by hand.
type schedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *schedClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *schedClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// While the wake is on, the scheduled check is only the fallback: it runs a
// pass once per supervisor_loop.wake.fallback_interval_minutes and each pass
// that runs is recorded. While the wake is off it runs on every tick.
func TestSupervisorLoopScheduledCheckHonoursFallbackInterval(t *testing.T) {
	var mu sync.Mutex
	set := supervisorloop.DefaultSettings()
	set.Wake.Enabled = true
	set.Wake.FallbackIntervalMinutes = 5
	live := func() supervisorloop.Settings { mu.Lock(); defer mu.Unlock(); return set }

	_, steps, store := newStepsRig(t, SupervisorLoopHooks{Settings: live})
	clock := &schedClock{now: time.Unix(1_700_000_000, 0)}
	steps.now = clock.Now
	rec := supervisorloop.NewWakeRecord(clock, 0)
	steps.SetWakeRecord(rec)
	ctx := context.Background()
	passes := func() int { return len(store.byJob(stepCloseFinishedRecords)) }

	if !steps.RunScheduled(ctx) || passes() != 1 {
		t.Fatalf("first scheduled check did not run a pass (passes=%d)", passes())
	}
	for _, step := range []time.Duration{time.Minute, time.Minute, time.Minute, 2*time.Minute - time.Second} {
		clock.advance(step)
		if steps.RunScheduled(ctx) {
			t.Fatalf("a pass ran before the 5 minute fallback interval had passed")
		}
	}
	if passes() != 1 || rec.Status().ScheduledCount != 1 {
		t.Fatalf("before the interval: passes=%d recorded=%d, want 1/1", passes(), rec.Status().ScheduledCount)
	}
	clock.advance(time.Second) // now exactly 5 minutes since the pass
	if !steps.RunScheduled(ctx) || passes() != 2 || rec.Status().ScheduledCount != 2 {
		t.Fatalf("at the interval: passes=%d recorded=%d, want 2/2", passes(), rec.Status().ScheduledCount)
	}
	if got := rec.Status().LastScheduledCheck; !got.Equal(clock.Now()) {
		t.Fatalf("last scheduled check = %v, want %v", got, clock.Now())
	}

	// The interval is read live: shortening it takes effect on the next check.
	mu.Lock()
	set.Wake.FallbackIntervalMinutes = 1
	mu.Unlock()
	clock.advance(time.Minute)
	if !steps.RunScheduled(ctx) || passes() != 3 {
		t.Fatalf("after shortening the interval to 1 minute no pass ran (passes=%d)", passes())
	}

	// Wake off: every tick runs a pass, as before the wake path existed.
	mu.Lock()
	set.Wake.Enabled = false
	mu.Unlock()
	for i := 0; i < 3; i++ {
		clock.advance(time.Second)
		if !steps.RunScheduled(ctx) {
			t.Fatalf("wake off: tick %d did not run a pass", i)
		}
	}
	if passes() != 6 || rec.Status().ScheduledCount != 6 {
		t.Fatalf("wake off: passes=%d recorded=%d, want 6/6", passes(), rec.Status().ScheduledCount)
	}

	// Loop disabled: nothing runs, nothing is recorded.
	mu.Lock()
	set.Enabled = false
	mu.Unlock()
	if steps.RunScheduled(ctx) || passes() != 6 {
		t.Fatalf("a pass ran while supervisor_loop.enabled is off")
	}
}

// The coordinator's idle tick goes through RunScheduled, so the interval gates
// the real scheduler and not only the helper.
func TestSupervisorLoopIdleTickUsesFallbackInterval(t *testing.T) {
	set := supervisorloop.DefaultSettings()
	set.Wake.Enabled = true
	set.Wake.FallbackIntervalMinutes = 60
	h, steps, store := newStepsRig(t, SupervisorLoopHooks{Settings: func() supervisorloop.Settings { return set }})
	h.c.SetSupervisorLoopSteps(steps)
	for i := 0; i < 5; i++ {
		h.c.supervisorLoopPass(context.Background())
	}
	if n := len(store.byJob(stepCloseFinishedRecords)); n != 1 {
		t.Fatalf("5 idle ticks inside a 60 minute fallback interval ran %d passes, want 1", n)
	}
}
