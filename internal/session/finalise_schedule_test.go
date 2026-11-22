package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

const finaliseScheduleDeadline = 5 * time.Second

// waitForCondition polls cond until it is true or the deadline passes.
func waitForCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(finaliseScheduleDeadline)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// shortenFinaliseIdleWait makes the idle re-read wait a few milliseconds for
// the duration of a test.
func shortenFinaliseIdleWait(t *testing.T) {
	t.Helper()
	old := finaliseIdleWait
	finaliseIdleWait = 2 * time.Millisecond
	t.Cleanup(func() { finaliseIdleWait = old })
}

// runFinaliseLoop runs the finaliser loop in a goroutine and registers a
// cleanup that cancels it and waits for it to exit. Call it after
// shortenFinaliseIdleWait so the wait is restored only once the loop is gone.
func runFinaliseLoop(t *testing.T, m *Manager, interval func() time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.finaliseLoop(ctx, interval)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(finaliseScheduleDeadline):
			t.Error("the finaliser loop did not stop at the end of the test")
		}
	})
}

// lastRunAt returns the time of the most recent finalise run, or the zero time
// before the first run.
func lastRunAt(m *Manager) time.Time {
	sum, _ := m.LastFinalise()
	return sum.At
}

// waitForNewRun waits until a finalise run newer than prev has been recorded
// and returns its time.
func waitForNewRun(t *testing.T, m *Manager, prev time.Time) time.Time {
	t.Helper()
	var got time.Time
	waitForCondition(t, "a finalise run", func() bool {
		got = lastRunAt(m)
		return got.After(prev)
	})
	return got
}

func TestFinaliserRunsOnceAtStartUp(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A long interval: only the start-up run can happen.
	var reads atomic.Int32
	mgr.StartFinaliser(ctx, func() time.Duration {
		reads.Add(1)
		return time.Hour
	})

	first := waitForNewRun(t, mgr, time.Time{})
	waitForCondition(t, "the loop to read the interval", func() bool { return reads.Load() >= 1 })
	// The interval is an hour, so no second run may be recorded.
	if got := lastRunAt(mgr); !got.Equal(first) {
		t.Fatalf("a second run happened within the interval: %v then %v", first, got)
	}
	if sum, ok := mgr.LastFinalise(); !ok || !sum.TerminalListOK {
		t.Fatalf("start-up run summary = %+v ok=%v, want a successful run", sum, ok)
	}
}

func TestFinaliserRunsOnSchedule(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.StartFinaliser(ctx, func() time.Duration { return 2 * time.Millisecond })

	at := waitForNewRun(t, mgr, time.Time{})
	for i := 0; i < 3; i++ {
		at = waitForNewRun(t, mgr, at)
	}
}

func TestFinaliserIntervalZeroSkipsScheduledRuns(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	shortenFinaliseIdleWait(t)

	var reads atomic.Int32
	runFinaliseLoop(t, mgr, func() time.Duration {
		reads.Add(1)
		return 0
	})

	startup := waitForNewRun(t, mgr, time.Time{})
	// The loop keeps re-reading the interval while running nothing.
	waitForCondition(t, "several interval re-reads", func() bool { return reads.Load() >= 5 })
	if got := lastRunAt(mgr); !got.Equal(startup) {
		t.Fatalf("a scheduled run happened with a zero interval: %v then %v", startup, got)
	}

	// A negative interval behaves the same way.
	mgr2, _ := newTestManagerWithFake(t)
	var reads2 atomic.Int32
	runFinaliseLoop(t, mgr2, func() time.Duration {
		reads2.Add(1)
		return -time.Second
	})
	startup2 := waitForNewRun(t, mgr2, time.Time{})
	waitForCondition(t, "several interval re-reads", func() bool { return reads2.Load() >= 5 })
	if got := lastRunAt(mgr2); !got.Equal(startup2) {
		t.Fatalf("a scheduled run happened with a negative interval: %v then %v", startup2, got)
	}
}

func TestFinaliserReadsIntervalEachCycle(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	shortenFinaliseIdleWait(t)

	// Start switched off, then turn it on, then off again, without a restart.
	var interval atomic.Int64
	var reads atomic.Int32
	runFinaliseLoop(t, mgr, func() time.Duration {
		reads.Add(1)
		return time.Duration(interval.Load())
	})

	at := waitForNewRun(t, mgr, time.Time{}) // start-up run
	waitForCondition(t, "interval re-reads while off", func() bool { return reads.Load() >= 3 })
	if got := lastRunAt(mgr); !got.Equal(at) {
		t.Fatalf("a run happened while the interval was zero: %v then %v", at, got)
	}

	interval.Store(int64(2 * time.Millisecond))
	at = waitForNewRun(t, mgr, at)
	at = waitForNewRun(t, mgr, at)

	interval.Store(0)
	// A run may already be in flight; once the loop has read the zero
	// interval twice more, nothing further can be scheduled.
	base := reads.Load()
	waitForCondition(t, "interval re-reads after switching off", func() bool { return reads.Load() >= base+3 })
	settled := lastRunAt(mgr)
	base = reads.Load()
	waitForCondition(t, "more interval re-reads", func() bool { return reads.Load() >= base+3 })
	if got := lastRunAt(mgr); !got.Equal(settled) {
		t.Fatalf("a run happened after the interval was set back to zero: %v then %v", settled, got)
	}
}

func TestFinaliserStopsWhenContextCancelled(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	ctx, cancel := context.WithCancel(context.Background())

	// The loop is waiting on a long timer when the context is cancelled: it
	// must return at once rather than at the end of the timer.
	done := make(chan struct{})
	go func() {
		defer close(done)
		mgr.finaliseLoop(ctx, func() time.Duration { return time.Hour })
	}()
	waitForNewRun(t, mgr, time.Time{})
	cancel()
	select {
	case <-done:
	case <-time.After(finaliseScheduleDeadline):
		t.Fatal("the finaliser loop did not stop after the context was cancelled")
	}

	// It also stops from the idle wait, and a cancelled context prevents any
	// run at all.
	shortenFinaliseIdleWait(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	var reads atomic.Int32
	go func() {
		defer close(done2)
		mgr.finaliseLoop(ctx2, func() time.Duration {
			reads.Add(1)
			return 0
		})
	}()
	waitForCondition(t, "the idle loop to cycle", func() bool { return reads.Load() >= 2 })
	cancel2()
	select {
	case <-done2:
	case <-time.After(finaliseScheduleDeadline):
		t.Fatal("the idle finaliser loop did not stop after the context was cancelled")
	}

	before := lastRunAt(mgr)
	ctx3, cancel3 := context.WithCancel(context.Background())
	cancel3()
	done3 := make(chan struct{})
	go func() {
		defer close(done3)
		mgr.finaliseLoop(ctx3, func() time.Duration { return time.Millisecond })
	}()
	select {
	case <-done3:
	case <-time.After(finaliseScheduleDeadline):
		t.Fatal("the finaliser loop did not return for an already cancelled context")
	}
	if got := lastRunAt(mgr); !got.Equal(before) {
		t.Fatalf("a run happened for an already cancelled context: %v then %v", before, got)
	}
}
