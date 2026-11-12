package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// The autonomous_config_set tool declares both merge-pending arguments and
// forwards them, presence-aware, into the nested automation object that
// PUT /api/autonomous/config reads.
func TestMergePendingConfigMCPForwardsKeys(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	names := configSetArgNames()
	for _, want := range []string{"automation_merge_pending_mode", "automation_merge_pending_alert_minutes"} {
		if !strings.Contains(names, want) {
			t.Errorf("autonomous_config_set does not declare %s", want)
		}
	}
	// An explicit 0 (alert off) is sent, not dropped.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_merge_pending_mode":          "shadow",
		"automation_merge_pending_alert_minutes": 0,
	})); err != nil {
		t.Fatalf("config set: %v", err)
	}
	auto, _ := rec.body["automation"].(map[string]any)
	if auto["merge_pending_mode"] != "shadow" {
		t.Errorf("automation = %v, want merge_pending_mode shadow", rec.body["automation"])
	}
	if got, ok := auto["merge_pending_alert_minutes"].(float64); !ok || got != 0 {
		t.Errorf("automation = %v, want merge_pending_alert_minutes 0", rec.body["automation"])
	}
	// Absent arguments send no merge-pending keys.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_idle_alert_minutes": 0,
	})); err != nil {
		t.Fatal(err)
	}
	auto, _ = rec.body["automation"].(map[string]any)
	for _, k := range []string{"merge_pending_mode", "merge_pending_alert_minutes"} {
		if _, ok := auto[k]; ok {
			t.Errorf("%s leaked into an idle-only PUT: %v", k, rec.body)
		}
	}
}
