package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// autonomous_config_set declares the shared-cache switch and forwards it,
// presence-aware, so an explicit false is sent.
func TestSharedCacheSweepMCPForwardsTheSwitch(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if !strings.Contains(configSetArgNames(), "automation_cleanup_shared_cache_sweep") {
		t.Fatal("autonomous_config_set does not declare automation_cleanup_shared_cache_sweep")
	}
	for _, want := range []bool{true, false} {
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
			"automation_cleanup_shared_cache_sweep": want,
		})); err != nil {
			t.Fatalf("config set: %v", err)
		}
		auto, _ := rec.body["automation"].(map[string]any)
		if got, ok := auto["cleanup_shared_cache_sweep"].(bool); !ok || got != want {
			t.Errorf("automation = %v, want cleanup_shared_cache_sweep %v", rec.body["automation"], want)
		}
	}
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_idle_alert_minutes": 0,
	})); err != nil {
		t.Fatal(err)
	}
	auto, _ := rec.body["automation"].(map[string]any)
	if _, ok := auto["cleanup_shared_cache_sweep"]; ok {
		t.Errorf("the switch leaked into an unrelated PUT: %v", rec.body)
	}
}
