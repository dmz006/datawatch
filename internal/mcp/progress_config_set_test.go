package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestProgressConfigMCPSetForwardsEveryKeyToConfigEndpoint(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"progress_lanes":                   0,
		"progress_inactive_gap_hours":      2.5,
		"progress_backlog_path":            "plans/backlog.md",
		"progress_since":                   "2026-01-02",
		"progress_snapshot_retention_days": 30,
	})); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Fatalf("proxied %s %s, want PUT /api/config", rec.method, rec.path)
	}
	want := map[string]any{
		"autonomous.progress.lanes":                   float64(0),
		"autonomous.progress.inactive_gap_hours":      2.5,
		"autonomous.progress.backlog_path":            "plans/backlog.md",
		"autonomous.progress.since":                   "2026-01-02",
		"autonomous.progress.snapshot_retention_days": float64(30),
	}
	for k, v := range want {
		if got, ok := rec.body[k]; !ok || got != v {
			t.Errorf("body[%s] = %v (present=%v), want %v", k, got, ok, v)
		}
	}
	names := configSetArgNames()
	for _, k := range []string{"progress_lanes", "progress_inactive_gap_hours", "progress_backlog_path", "progress_since", "progress_snapshot_retention_days"} {
		if !strings.Contains(names, k) {
			t.Errorf("autonomous_config_set does not declare %s", k)
		}
	}
}
