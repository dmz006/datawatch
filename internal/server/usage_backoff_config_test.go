// BL428 Phase 4 — autonomous.usage_backoff / autonomous.backoff_max_wait_minutes
// reachable and validated on both config surfaces (PUT /api/config in the
// CLI / comm / MCP config_set shape, and PUT /api/autonomous/config in the
// MCP autonomous_config_set shape).

package server

import (
	"os"
	"strings"
	"testing"
)

const (
	ubModeKey    = "autonomous.usage_backoff"
	ubMaxWaitKey = "autonomous.backoff_max_wait_minutes"
	ubModeArg    = "usage_backoff"
	ubMaxWaitArg = "backoff_max_wait_minutes"
)

func TestUsageBackoffConfig_Surfaces(t *testing.T) {
	s, mgr, cfgPath := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)

	// Unset config shows the effective defaults.
	aut := getAutonomousConfigMap(t, s)
	if g := string(aut[ubModeArg]); g != `"pause_backend"` {
		t.Fatalf("default %s = %s, want \"pause_backend\"", ubModeKey, g)
	}
	if g := string(aut[ubMaxWaitArg]); g != "60" {
		t.Fatalf("default %s = %s, want 60", ubMaxWaitKey, g)
	}

	// PUT /api/config in the comm `configure` shape: a bare number for the
	// minutes (main.go sends integers unquoted) and a string for the mode.
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{ubModeKey: "off", ubMaxWaitKey: 15})
	aut = getAutonomousConfigMap(t, s)
	if g := string(aut[ubModeArg]); g != `"off"` {
		t.Fatalf("after PUT: %s = %s, want \"off\"", ubModeKey, g)
	}
	if g := string(aut[ubMaxWaitArg]); g != "15" {
		t.Fatalf("after PUT: %s = %s, want 15", ubMaxWaitKey, g)
	}
	if m := mgr.Config(); m.UsageBackoff != "off" || m.BackoffMaxWaitMinutes != 15 {
		t.Fatalf("live manager after PUT = %q / %d, want off / 15", m.UsageBackoff, m.BackoffMaxWaitMinutes)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "usage_backoff: \"off\"") && !strings.Contains(string(raw), "usage_backoff: off") {
		t.Fatalf("config.yaml lacks usage_backoff:\n%s", raw)
	}
	if !strings.Contains(string(raw), "backoff_max_wait_minutes: 15") {
		t.Fatalf("config.yaml lacks backoff_max_wait_minutes:\n%s", raw)
	}

	// Bare-key form on /api/autonomous/config — the autonomous_config_set shape.
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{ubModeArg: "pause_backend", ubMaxWaitArg: 45})
	aut = getAutonomousConfigMap(t, s)
	if g := string(aut[ubModeArg]); g != `"pause_backend"` {
		t.Fatalf("after autonomous-config PUT: mode = %s", g)
	}
	if g := string(aut[ubMaxWaitArg]); g != "45" {
		t.Fatalf("after autonomous-config PUT: max wait = %s", g)
	}
	if m := mgr.Config(); m.UsageBackoff != "pause_backend" || m.BackoffMaxWaitMinutes != 45 {
		t.Fatalf("live manager after autonomous-config PUT = %q / %d", m.UsageBackoff, m.BackoffMaxWaitMinutes)
	}

	// An update to one key leaves the other alone, and null is "absent".
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{ubModeArg: nil, ubMaxWaitArg: nil})
	if m := mgr.Config(); m.UsageBackoff != "pause_backend" || m.BackoffMaxWaitMinutes != 45 {
		t.Fatalf("null changed the config: %q / %d", m.UsageBackoff, m.BackoffMaxWaitMinutes)
	}

	// The manager's own status mirrors the effective config.
	if st := mgr.Status(); st.UsageBackoff == nil || st.UsageBackoff.Config.Mode != "pause_backend" || st.UsageBackoff.Config.MaxWaitMinutes != 45 {
		t.Fatalf("autonomous status usage_backoff = %+v", st.UsageBackoff)
	}

	// Invalid values are rejected on BOTH surfaces and change nothing.
	for _, bad := range []map[string]any{
		{ubModeKey: "bogus"},
		{ubModeKey: true},
		{ubMaxWaitKey: 0},
		{ubMaxWaitKey: -5},
		{ubMaxWaitKey: 1.5},
		{ubMaxWaitKey: "soon"},
		{ubMaxWaitKey: 99999999},
	} {
		pcPutBad(t, s.handlePutConfig, "/api/config", bad)
	}
	for _, bad := range []map[string]any{
		{ubModeArg: "bogus"},
		{ubModeArg: 7},
		{ubMaxWaitArg: 0},
		{ubMaxWaitArg: 2.5},
		{ubMaxWaitArg: "oops"},
	} {
		pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", bad)
	}
	if m := mgr.Config(); m.UsageBackoff != "pause_backend" || m.BackoffMaxWaitMinutes != 45 {
		t.Fatalf("rejected PUTs changed the config: %q / %d", m.UsageBackoff, m.BackoffMaxWaitMinutes)
	}
}
