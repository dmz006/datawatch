package main

import (
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
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
	if err := adm.HoldNewWork("claude-code", true); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := bo.Blocked("claude-code"); !blocked {
		t.Fatal("hold did not block new claude-code work")
	}
	if err := adm.HoldNewWork("claude-code", false); err != nil {
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

func TestFillBurnGuardStats(t *testing.T) {
	var s statspkg.SystemStats
	fillBurnGuardStats(&s, server.BurnGuardStats{State: "guarding", Level: 2, Projected: 104, Counters: server.BurnGuardCounters{Ticks: 5, Climbs: 2}})
	if s.BurnGuardState != "guarding" || s.BurnGuardLevel != 2 || s.BurnGuardTicks != 5 || s.BurnGuardClimbs != 2 {
		t.Fatalf("stats = %+v", s)
	}
	updateBurnGuardMetrics(s)
	_ = time.Now
}
