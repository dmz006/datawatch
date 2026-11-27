// The cleanup_archived switch over MCP: declared on autonomous_config_set and
// forwarded only when sent.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCleanupArchivedParity_MCP(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if !strings.Contains(configSetArgNames(), "automation_cleanup_archived") {
		t.Fatal("autonomous_config_set does not declare automation_cleanup_archived")
	}
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"automation_cleanup_archived": false})); err != nil {
		t.Fatal(err)
	}
	auto, _ := rec.body["automation"].(map[string]any)
	if v, ok := auto["cleanup_archived"].(bool); !ok || v {
		t.Fatalf("explicit false not forwarded: %v", rec.body)
	}
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"automation_idle_alert_minutes": 0})); err != nil {
		t.Fatal(err)
	}
	auto, _ = rec.body["automation"].(map[string]any)
	if _, ok := auto["cleanup_archived"]; ok {
		t.Fatalf("sent without being asked: %v", rec.body)
	}
}
