package main

import (
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
	statspkg "github.com/dmz006/datawatch/internal/stats"
)

func TestBurnGuardAdmissionLowersAndRestoresInflight(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode, MaxInflight: 3}); err != nil {
		t.Fatal(err)
	}
	led := capacity.New(capacity.Options{})
	bo := autonomouspkg.NewBackoff()
	adm := burnGuardAdmission{cfg: cfg, ledger: led, bo: bo, reg: func() *inference.Registry { return reg }}
	t.Cleanup(func() { burnGuardInflight.clear("claude-code") })

	if got := adm.InflightCap("claude-code"); got != 3 {
		t.Fatalf("cap = %d, want 3", got)
	}
	if err := adm.SetInflight("claude-code", 2); err != nil {
		t.Fatal(err)
	}
	// The 10s limit sync must not undo the lowered cap.
	syncLLMLimits(cfg, reg, led)
	if got := led.Limit("llm:claude-code"); got != 2 {
		t.Fatalf("ledger limit after sync = %d, want 2", got)
	}
	if got := adm.InflightCap("claude-code"); got != 2 {
		t.Fatalf("cap = %d, want 2", got)
	}
	if err := adm.SetInflight("claude-code", 3); err != nil {
		t.Fatal(err)
	}
	syncLLMLimits(cfg, reg, led)
	if got := led.Limit("llm:claude-code"); got != 3 {
		t.Fatalf("restored limit = %d, want 3", got)
	}
}

func TestBurnGuardAdmissionHoldAndRoute(t *testing.T) {
	bo := autonomouspkg.NewBackoff()
	adm := burnGuardAdmission{bo: bo}
	if err := adm.HoldNewWork("claude-code", burnguard.RungHoldEntry, true); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := bo.Blocked("claude-code"); !blocked {
		t.Fatal("hold did not block new claude-code work")
	}
	if es := bo.Active(); len(es) != 1 || es[0].Rung != burnguard.RungHoldEntry {
		t.Fatalf("flag rung = %v, want the hold rung the hold named", es)
	}
	if err := adm.HoldNewWork("claude-code", burnguard.RungHoldEntry, false); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := bo.Blocked("claude-code"); blocked {
		t.Fatal("release left the hold in place")
	}
	_ = adm.RouteBackend("claude-code", "pi-prompt", true)
	if to, ok := burnGuardRoutes.RouteFor("claude-code"); !ok || to != "pi-prompt" {
		t.Fatalf("route = %q %v", to, ok)
	}
	_ = adm.RouteBackend("claude-code", "pi-prompt", false)
	if _, ok := burnGuardRoutes.RouteFor("claude-code"); ok {
		t.Fatal("route not lifted")
	}
}

// setLocalZone installs loc as time.Local and restores the previous value after the test.
func setLocalZone(t *testing.T, loc *time.Location) {
	t.Helper()
	prev := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = prev })
}

// TestBurnGuardHoldDeadlineTracksResetAndZone: the hold flag's deadline is the earlier of the
// next weekly reset and the next guard reading, it is never a fixed long span, and it carries
// the daemon's own zone offset.
func TestBurnGuardHoldDeadlineTracksResetAndZone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York unavailable: %v", err)
	}
	setLocalZone(t, loc)

	cfg := config.DefaultConfig()
	cfg.Usage.Calculator.ResetWeekday = "sunday"
	cfg.Usage.Calculator.ResetTime = "08:00"
	cfg.Usage.Calculator.ResetZone = "America/New_York"
	cfg.Usage.BurnGuard.IntervalMinutes = 60

	now := time.Date(2026, 11, 4, 4, 0, 0, 0, loc) // Wednesday; the next Sunday 08:00 reset is days out
	bo := autonomouspkg.NewBackoff()
	adm := burnGuardAdmission{cfg: cfg, bo: bo, now: func() time.Time { return now }}
	if err := adm.HoldNewWork("claude-code", burnguard.RungHoldEntry, true); err != nil {
		t.Fatal(err)
	}
	es := bo.Active()
	if len(es) != 1 {
		t.Fatalf("entries = %d, want 1", len(es))
	}
	e := es[0]
	next, err := server.UsageCalculatorSettings(cfg.Usage).Reset.NextReset(now)
	if err != nil {
		t.Fatalf("reset spec does not resolve: %v", err)
	}
	want := now.Add(time.Hour)
	if next.Before(want) {
		want = next
	}
	if !e.Until.Equal(want) {
		t.Fatalf("until = %v, want %v (the earlier of the reset %v and the next reading)", e.Until, want, next)
	}
	if e.Until.After(next) {
		t.Fatalf("until %v is after the next weekly reset %v", e.Until, next)
	}
	if e.Until.After(now.Add(24 * time.Hour)) {
		t.Fatalf("until %v outlives the guard's cadence; the hold spans at most one reset or one reading, never a fixed long span", e.Until)
	}
	if !e.ResetKnown {
		t.Fatal("reset_known = false, want true (the reset spec resolves)")
	}
	_, gotOff := e.Until.Zone()
	_, wantOff := now.Zone()
	if gotOff != wantOff {
		t.Fatalf("until carries offset %d, want %d (the deadline carries the daemon's zone, not a foreign one)", gotOff, wantOff)
	}
	if e.Until.Location().String() != loc.String() {
		t.Fatalf("location = %v, want %v", e.Until.Location(), loc)
	}
}

// TestBurnGuardHoldDeadlineNextReadingWhenEarlier: when the guard's next reading comes before
// the weekly reset, the deadline is that reading (now plus the tick interval).
func TestBurnGuardHoldDeadlineNextReadingWhenEarlier(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York unavailable: %v", err)
	}
	setLocalZone(t, loc)

	cfg := config.DefaultConfig()
	cfg.Usage.Calculator.ResetWeekday = "sunday"
	cfg.Usage.Calculator.ResetTime = "08:00"
	cfg.Usage.Calculator.ResetZone = "America/New_York"
	cfg.Usage.BurnGuard.IntervalMinutes = 15

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, loc) // Monday noon; the next reset is six days out
	bo := autonomouspkg.NewBackoff()
	adm := burnGuardAdmission{cfg: cfg, bo: bo, now: func() time.Time { return now }}
	if err := adm.HoldNewWork("claude-code", burnguard.RungHoldEntry, true); err != nil {
		t.Fatal(err)
	}
	es := bo.Active()
	if len(es) != 1 {
		t.Fatalf("entries = %d, want 1", len(es))
	}
	e := es[0]
	want := now.Add(15 * time.Minute)
	if !e.Until.Equal(want) {
		t.Fatalf("until = %v, want the next reading %v", e.Until, want)
	}
	if !e.ResetKnown {
		t.Fatal("reset_known = false, want true (the reset spec resolves)")
	}
}

// TestBurnGuardHoldDeadlineUnresolvableReset: when no settings are available to resolve a
// reset, the deadline is now plus the default tick interval and reset_known is false.
func TestBurnGuardHoldDeadlineUnresolvableReset(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York unavailable: %v", err)
	}
	setLocalZone(t, loc)

	now := time.Date(2026, 11, 4, 4, 0, 0, 0, loc)
	bo := autonomouspkg.NewBackoff()
	adm := burnGuardAdmission{bo: bo, now: func() time.Time { return now }} // no config: no reset spec
	if err := adm.HoldNewWork("claude-code", burnguard.RungHoldEntry, true); err != nil {
		t.Fatal(err)
	}
	es := bo.Active()
	if len(es) != 1 {
		t.Fatalf("entries = %d, want 1", len(es))
	}
	e := es[0]
	want := now.Add(time.Duration(config.BurnGuardIntervalMinutesDefault) * time.Minute)
	if !e.Until.Equal(want) {
		t.Fatalf("until = %v, want the next reading %v", e.Until, want)
	}
	if e.ResetKnown {
		t.Fatal("reset_known = true, want false (the reset could not be resolved)")
	}
	_, gotOff := e.Until.Zone()
	_, wantOff := now.Zone()
	if gotOff != wantOff {
		t.Fatalf("until carries offset %d, want %d", gotOff, wantOff)
	}
}

func TestFillBurnGuardStats(t *testing.T) {
	var s statspkg.SystemStats
	fillBurnGuardStats(&s, server.BurnGuardStats{State: "guarding", Level: 2, Projected: 104, Counters: server.BurnGuardCounters{Ticks: 5, Climbs: 2}})
	if s.BurnGuardState != "guarding" || s.BurnGuardLevel != 2 || s.BurnGuardTicks != 5 || s.BurnGuardClimbs != 2 {
		t.Fatalf("stats = %+v", s)
	}
	updateBurnGuardMetrics(s)
	_ = time.Now
}

// The pool limit follows the entry's effective cap. The guard's lowered cap is
// kept only while the entry still says what it said when the guard lowered it:
// an operator who changes the entry afterwards gets that value in the pool, and
// changing it again keeps following (item 27).
func TestBurnGuardInflightOverride_PoolFollowsTheEntryAfterTheOperatorChangesIt(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := inference.NewRegistry()
	entry := &inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode, MaxInflight: 2}
	if err := reg.Add(entry); err != nil {
		t.Fatal(err)
	}
	led := capacity.New(capacity.Options{})
	adm := burnGuardAdmission{cfg: cfg, ledger: led, bo: autonomouspkg.NewBackoff(), reg: func() *inference.Registry { return reg }}
	t.Cleanup(func() { burnGuardInflight.clear("claude-code") })

	// The guard lowers the cap from 2 to 1 and the pool follows.
	if err := adm.SetInflight("claude-code", 1); err != nil {
		t.Fatal(err)
	}
	syncLLMLimits(cfg, reg, led)
	if got := led.Limit("llm:claude-code"); got != 1 {
		t.Fatalf("pool while the guard holds the lowered cap = %d, want 1", got)
	}
	// The operator raises the entry to 3, then sets it back to 2: each value
	// reaches the pool on the next sync, whatever the guard lowered earlier.
	for _, want := range []int{3, 2} {
		entry.MaxInflight = want
		if err := reg.Update(entry); err != nil {
			t.Fatal(err)
		}
		syncLLMLimits(cfg, reg, led)
		if got := led.Limit("llm:claude-code"); got != want {
			t.Fatalf("after the operator set the entry to %d the pool is %d", want, got)
		}
		if got := adm.InflightCap("claude-code"); got != want {
			t.Fatalf("after the operator set the entry to %d the guard reads %d", want, got)
		}
	}
}
