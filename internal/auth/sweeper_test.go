// F10 sprint 5 (S5.5) — sweeper tests.

package auth

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// RunSweeper runs one sweep immediately on start, regardless of
// interval — verify by injecting an orphan and using a result hook.
func TestRunSweeper_FirstTickImmediate(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(filepath.Join(dir, "tok.json"))
	b := &TokenBroker{Provider: &fakeProvider{}, Store: store}

	// Seed an orphaned token (no worker in active set).
	store.mu.Lock()
	_ = store.put(&TokenRecord{
		WorkerID: "ghost", Repo: "x/y", Token: "tok-ghost",
		IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	})
	store.mu.Unlock()

	swept := make(chan int, 4)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = RunSweeper(ctx, SweeperConfig{
			Broker:        b,
			ActiveIDsFn:   func() []string { return nil },
			Interval:      time.Hour, // ensure only the immediate tick fires
			OnSweepResult: func(n int) { swept <- n },
		})
	}()

	select {
	case n := <-swept:
		if n != 1 {
			t.Errorf("first sweep removed %d, want 1", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first sweep didn't fire")
	}
	cancel()

	// And the store should be empty now.
	if got := store.Get("ghost"); got != nil {
		t.Errorf("ghost token survived sweep: %+v", got)
	}
}

// RunSweeper honours ctx cancellation — no goroutine leak.
func TestRunSweeper_CancelStops(t *testing.T) {
	store, _ := NewTokenStore(filepath.Join(t.TempDir(), "tok.json"))
	b := &TokenBroker{Provider: &fakeProvider{}, Store: store}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = RunSweeper(ctx, SweeperConfig{
			Broker:      b,
			ActiveIDsFn: func() []string { return nil },
			Interval:    50 * time.Millisecond,
		})
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunSweeper did not stop after ctx cancel")
	}
}

// RunSweeper periodically re-runs at Interval cadence.
func TestRunSweeper_PeriodicTicks(t *testing.T) {
	store, _ := NewTokenStore(filepath.Join(t.TempDir(), "tok.json"))
	b := &TokenBroker{Provider: &fakeProvider{}, Store: store}

	var ticks int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = RunSweeper(ctx, SweeperConfig{
			Broker:        b,
			ActiveIDsFn:   func() []string { return nil },
			Interval:      30 * time.Millisecond,
			OnSweepResult: func(_ int) { atomic.AddInt32(&ticks, 1) },
		})
	}()
	time.Sleep(140 * time.Millisecond)
	got := atomic.LoadInt32(&ticks)
	if got < 3 {
		t.Errorf("ticks=%d want ≥3 (immediate + 3+ periodic)", got)
	}
}

// Required-arg validation.
func TestRunSweeper_ConfigValidation(t *testing.T) {
	if err := RunSweeper(context.Background(), SweeperConfig{}); err == nil {
		t.Error("expected error for empty config")
	}
	if err := RunSweeper(context.Background(), SweeperConfig{
		Broker: &TokenBroker{},
	}); err == nil {
		t.Error("expected error for missing ActiveIDsFn")
	}
}
