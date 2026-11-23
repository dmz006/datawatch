package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

const finaliserWiringDeadline = 5 * time.Second

func newFinaliserWiringManager(t *testing.T) (*session.Manager, *session.FakeTmux) {
	t.Helper()
	m, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTestTemp(m)
	trackSessionManager(t, m)
	return m, m.WithFakeTmux()
}

func waitForFinaliserCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(finaliserWiringDeadline)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestStartSessionFinaliserReadsIntervalFromConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.FinaliseIntervalMinutes = 10
	var reads atomic.Int32
	minutes := func() int {
		reads.Add(1)
		return cfg.Session.FinaliseIntervalMinutes
	}

	// The interval function re-reads the source on every call, so changing
	// the minutes changes the next wait.
	interval := finaliserInterval(minutes)
	if got := interval(); got != 10*time.Minute {
		t.Fatalf("interval = %v, want 10m", got)
	}
	cfg.Session.FinaliseIntervalMinutes = 3
	if got := interval(); got != 3*time.Minute {
		t.Fatalf("interval after change = %v, want 3m", got)
	}
	cfg.Session.FinaliseIntervalMinutes = 0
	if got := interval(); got != 0 {
		t.Fatalf("interval for 0 minutes = %v, want 0", got)
	}

	// The started finaliser reads the source after its start-up run.
	mgr, _ := newFinaliserWiringManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads.Store(0)
	cfg.Session.FinaliseIntervalMinutes = 60
	startSessionFinaliser(ctx, mgr, minutes)
	waitForFinaliserCondition(t, "the finaliser to read the minutes", func() bool { return reads.Load() >= 1 })
}

func TestStartSessionFinaliserRunsAtStartUp(t *testing.T) {
	mgr, _ := newFinaliserWiringManager(t)

	// A running record whose terminal is gone (the fake lists none).
	const full = "testhost-gone-1"
	if err := mgr.SaveSession(&session.Session{
		ID: "gone-1", FullID: full, Hostname: "testhost", TmuxSession: "cs-gone-1",
		State: session.StateRunning, UpdatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// An hour between runs: only the start-up run can finalise the record.
	startSessionFinaliser(ctx, mgr, func() int { return 60 })

	waitForFinaliserCondition(t, "the start-up run", func() bool {
		_, ran := mgr.LastFinalise()
		return ran
	})
	got, ok := mgr.GetSession(full)
	if !ok {
		t.Fatalf("record %s missing after the start-up run", full)
	}
	if got.State != session.StateKilled {
		t.Fatalf("state = %q, want %q", got.State, session.StateKilled)
	}
}
