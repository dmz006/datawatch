package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// paceTestLedger is a ledger whose role gate is the daemon's: the pace hold in front of the
// back-off reserve. The claude-code pool has room for three sessions, so only the hold can
// make a request wait.
func paceTestLedger(ph *paceHolds) *capacity.Ledger {
	bo := autonomous.NewBackoff()
	led := capacity.New(capacity.Options{
		PollInterval:     5 * time.Millisecond,
		RoleBackpressure: paceHoldBackpressure(ph, backoffReserveBackpressure(bo, backoffReserveGate(&config.Config{}))),
	})
	led.SetLimit("llm:claude-code", 3)
	led.SetLimit("host", 10)
	return led
}

var paceClaudePools = []string{"llm:claude-code"}

// admitsNow reports whether a request is admitted without waiting.
func admitsNow(led *capacity.Ledger, req capacity.Request) (bool, string) {
	var reason string
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	go func() {
		done <- led.Acquire(ctx, req, 150*time.Millisecond, nil, func(r string) { reason = r })
	}()
	err := <-done
	if err == nil {
		led.Release(req.Holder)
	}
	return err == nil, reason
}

func TestPaceHold_WorkersWaitOperatorSessionsAndReviewersDoNot(t *testing.T) {
	ph := newPaceHolds(time.Now)
	led := paceTestLedger(ph)
	ph.Set("claude-code", "burn guard pace policy: window_pause_line: window estimate 88.0%", time.Now().Add(time.Hour))

	if ok, why := admitsNow(led, capacity.Request{Holder: "w1", Role: capacity.RoleWorker, Pools: paceClaudePools}); ok || !strings.Contains(why, "burn guard pace hold") || !strings.Contains(why, "88.0") {
		t.Fatalf("a new worker must wait with the pace hold as its reason: admitted=%v reason=%q", ok, why)
	}
	// An operator-started session is accounted but never held (so is a supervisor session,
	// which starts the same way).
	if ok, why := admitsNow(led, capacity.Request{Holder: "op", Role: capacity.RoleWorker, Pools: paceClaudePools, Interactive: true}); !ok {
		t.Fatalf("an operator session must start through a pace hold: %q", why)
	}
	// Work already in flight finishes: its verifier and judge are not held.
	if ok, why := admitsNow(led, capacity.Request{Holder: "v1", Role: capacity.RoleVerifier, Pools: paceClaudePools}); !ok {
		t.Fatalf("a verifier must not be held by the pace hold: %q", why)
	}
	// Another backend is untouched.
	if ok, _ := admitsNow(led, capacity.Request{Holder: "w2", Role: capacity.RoleWorker, Pools: []string{"llm:pi"}}); !ok {
		t.Fatal("the hold is on claude-code only")
	}
	// A pool that is not an llm pool is never gated.
	if ok, _ := admitsNow(led, capacity.Request{Holder: "w3", Role: capacity.RoleWorker, Pools: []string{"host"}}); !ok {
		t.Fatal("host pool must never be gated by the pace hold")
	}
}

func TestPaceHold_LiftsOnClearAndOnExpiry(t *testing.T) {
	now := time.Date(2026, 10, 30, 20, 0, 0, 0, time.UTC)
	clk := &backoffTestClock{now: now}
	ph := newPaceHolds(clk.Now)
	led := paceTestLedger(ph)
	req := capacity.Request{Holder: "w", Role: capacity.RoleWorker, Pools: paceClaudePools}

	ph.Set("claude-code", "why", now.Add(2*time.Hour))
	if ok, _ := admitsNow(led, req); ok {
		t.Fatal("held")
	}
	ph.Clear("claude-code")
	if ok, _ := admitsNow(led, req); !ok {
		t.Fatal("a cleared hold must lift")
	}
	// A guard that stops ticking stops renewing the hold: it lapses at its deadline.
	ph.Set("claude-code", "why", now.Add(2*time.Hour))
	clk.Advance(2*time.Hour + time.Second)
	if ok, _ := admitsNow(led, req); !ok {
		t.Fatal("an expired hold must lapse")
	}
	if got := ph.List(); len(got) != 0 {
		t.Fatalf("an expired hold must not be listed: %+v", got)
	}
}

func TestPaceHold_NilRegistryChangesNothing(t *testing.T) {
	gate := paceHoldBackpressure(nil, nil)
	if b, _ := gate("llm:claude-code", capacity.RoleWorker, nil); b {
		t.Fatal("no registry, no hold")
	}
}

// paceE2E drives the real runner with a fake clock and fake readings against the daemon's
// pace-hold seam and a ledger that uses its gate.
type paceE2E struct {
	g    *server.BurnGuard
	led  *capacity.Ledger
	ph   *paceHolds
	cfg  *config.BurnGuardConfig
	mu   sync.Mutex
	week float64
}

type quietNotifier struct{}

func (quietNotifier) Alert(alerts.Level, string, string) {}
func (quietNotifier) Question(string) error              { return nil }

func newPaceE2E(t *testing.T, mode string) *paceE2E {
	t.Helper()
	cfg := config.DefaultBurnGuard()
	cfg.MaxProjectionGapPoints = 0
	cfg.PaceLineMode = mode
	cfg.Ladder = []string{burnguard.RungLowerInflight}
	e := &paceE2E{cfg: &cfg, ph: newPaceHolds(time.Now), week: 97}
	e.led = paceTestLedger(e.ph)
	now := time.Now().UTC().Truncate(time.Minute)
	// The weekly reset sits two days ahead, so the pace line is at 5/7 of the way through.
	reset := usagecalc.ResetSpec{Weekday: now.Add(48 * time.Hour).Weekday(), Clock: now.Add(48 * time.Hour).Format("15:04"), Zone: "UTC"}
	adm := burnGuardAdmission{cfg: &config.Config{}, ledger: e.led, bo: autonomous.NewBackoff(), pace: e.ph}
	in := server.BurnGuardInputs{
		Config: func() config.BurnGuardConfig { return cfg },
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			v := e.week
			return []usagecalc.Reading{{At: now.Add(-10 * time.Minute), Percent: &v}}, 0, reset, nil
		},
		Tokens: func(context.Context, string, bool, time.Time) ([]burnguard.Bucket, time.Time, error) {
			return nil, now, nil
		},
		Now:          func() time.Time { return now },
		MarginPoints: func() *float64 { v := 0.0; return &v },
	}
	g, err := server.NewBurnGuard(in, adm, nil, quietNotifier{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.g = g
	return e
}

func (e *paceE2E) setWeek(v float64) { e.mu.Lock(); e.week = v; e.mu.Unlock() }

var paceWorker = capacity.Request{Holder: "worker", Role: capacity.RoleWorker, Pools: paceClaudePools}

// TestBurnGuardPace_EnforceHoldsAdmissionShadowDoesNot is the proof the mode matters: the same
// reading, over the weekly pace line, holds a new worker in on and never in shadow.
func TestBurnGuardPace_EnforceHoldsAdmissionShadowDoesNot(t *testing.T) {
	shadow := newPaceE2E(t, "shadow")
	shadow.g.Tick(context.Background())
	if ok, why := admitsNow(shadow.led, paceWorker); !ok {
		t.Fatalf("shadow held a worker: %q", why)
	}
	if p := shadow.g.Status().Pace; p == nil || len(p.Log) == 0 || p.Log[0].Action != burnguard.PaceActionWouldHold {
		t.Fatalf("shadow must still record the decision: %+v", p)
	}

	on := newPaceE2E(t, "on")
	on.g.Tick(context.Background())
	ok, why := admitsNow(on.led, paceWorker)
	if ok || !strings.Contains(why, "weekly_pace_line") {
		t.Fatalf("enforce must hold a new worker with the rule as the reason: admitted=%v reason=%q", ok, why)
	}
	// A hold lifts when the reading drops.
	on.setWeek(40)
	on.g.Tick(context.Background())
	if ok, why := admitsNow(on.led, paceWorker); !ok {
		t.Fatalf("the hold must lift once the reading drops: %q", why)
	}
}
