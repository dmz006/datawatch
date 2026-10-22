package main

import (
	"testing"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type auditRec struct {
	action  string
	details map[string]any
}

func recordAudit(into *[]auditRec) backoffAuditFunc {
	return func(action string, details map[string]any) {
		*into = append(*into, auditRec{action, details})
	}
}

func TestRateLimitBackoffHandler_ResetTimeSetsFlagAndAudits(t *testing.T) {
	cfg := &config.Config{}
	bo := autonomouspkg.NewBackoff()
	var events []auditRec
	h := rateLimitBackoffHandler(cfg, bo, time.Now, recordAudit(&events))

	reset := time.Now().Add(90 * time.Minute).UTC().Truncate(time.Second)
	h("claude-code", reset, true)

	act := bo.Active()
	if len(act) != 1 || act[0].Backend != "claude-code" || !act[0].Until.Equal(reset) || act[0].Source != autonomouspkg.BackoffSourceRateLimit {
		t.Fatalf("active = %+v", act)
	}
	if len(events) != 1 || events[0].action != auditpkg.ActionUsageBackoffSet {
		t.Fatalf("events = %+v", events)
	}
	d := events[0].details
	if d["backend"] != "claude-code" || d["source"] != "rate_limit" || d["until"] != reset.Format(time.RFC3339) {
		t.Errorf("details = %+v", d)
	}
}

func TestRateLimitBackoffHandler_NoResetUsesMaxBackoff(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.BackoffMaxWaitMinutes = 25
	bo := autonomouspkg.NewBackoff()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	var events []auditRec
	h := rateLimitBackoffHandler(cfg, bo, func() time.Time { return now }, recordAudit(&events))

	h("pi", time.Time{}, false)

	act := bo.Active()
	if len(act) != 1 || !act[0].Until.Equal(now.Add(25*time.Minute)) {
		t.Fatalf("active = %+v, want until now+25m", act)
	}
	if len(events) != 1 {
		t.Errorf("want 1 audit event, got %+v", events)
	}
}

func TestRateLimitBackoffHandler_OffModeSkipsAutomaticPath(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.UsageBackoff = "off"
	bo := autonomouspkg.NewBackoff()
	var events []auditRec
	h := rateLimitBackoffHandler(cfg, bo, time.Now, recordAudit(&events))

	h("claude-code", time.Now().Add(time.Hour), true)
	if len(bo.Active()) != 0 || len(events) != 0 {
		t.Fatalf("off mode must not set a flag or audit: active=%+v events=%+v", bo.Active(), events)
	}

	// Config changes apply to the same listener without a restart.
	cfg.Autonomous.UsageBackoff = "pause_backend"
	h("claude-code", time.Now().Add(time.Hour), true)
	if len(bo.Active()) != 1 {
		t.Fatalf("flipping to pause_backend should enable the listener, active=%+v", bo.Active())
	}
}

func TestRateLimitBackoffHandler_LeavesOperatorFlagsAlone(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.UsageBackoff = "off"
	bo := autonomouspkg.NewBackoff()
	bo.Set("pi", time.Now().Add(time.Hour), "operator", autonomouspkg.BackoffSourceOperator)
	h := rateLimitBackoffHandler(cfg, bo, time.Now, func(string, map[string]any) {})
	h("pi", time.Now().Add(2*time.Hour), true)
	if act := bo.Active(); len(act) != 1 || act[0].Source != autonomouspkg.BackoffSourceOperator {
		t.Fatalf("operator flag changed in off mode: %+v", act)
	}
}

func TestSweepBackoff_AuditsAutoCleared(t *testing.T) {
	bo := autonomouspkg.NewBackoff()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	bo.Set("old", now.Add(-time.Minute), "r", autonomouspkg.BackoffSourceRateLimit)
	bo.Set("live", now.Add(time.Hour), "r", autonomouspkg.BackoffSourceRateLimit)
	var events []auditRec
	sweepBackoff(bo, now, recordAudit(&events))

	if len(events) != 1 || events[0].action != auditpkg.ActionUsageBackoffAutoCleared {
		t.Fatalf("events = %+v", events)
	}
	if events[0].details["backend"] != "old" || events[0].details["until"] != now.Add(-time.Minute).Format(time.RFC3339) {
		t.Errorf("details = %+v", events[0].details)
	}
	if act := bo.Active(); len(act) != 1 || act[0].Backend != "live" {
		t.Errorf("active after sweep = %+v", act)
	}
}

func TestEmitBackoffAudit_NoSinkIsNoop(t *testing.T) {
	setBackoffAuditLog(nil)
	emitBackoffAudit(auditpkg.ActionUsageBackoffSet, map[string]any{"backend": "x"}) // must not panic
}
