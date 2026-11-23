package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestDormantFeaturesMCPToolShape(t *testing.T) {
	tool := (&Server{}).toolAutonomousDormantFeatures()
	if tool.Name != "autonomous_dormant_features" {
		t.Errorf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("must be read-only")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("takes no arguments, schema has %v", tool.InputSchema.Properties)
	}
	if got := federation.MCPToolCap["autonomous_dormant_features"]; got != federation.CapAutonomousRead {
		t.Errorf("capability = %q", got)
	}
	if got := DormantFeaturesToolNames(); len(got) != 1 || got[0] != "autonomous_dormant_features" {
		t.Errorf("names = %v", got)
	}
}

func TestDormantFeaturesMCPProxiesGet(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"features":[],"marker":7}`))
	})
	res, err := s.handleAutonomousDormantFeatures(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/dormant-features" {
		t.Errorf("proxied %s %s", method, path)
	}
	if !strings.Contains(automationText(t, res), `"marker":7`) {
		t.Errorf("body not passed through: %s", automationText(t, res))
	}
}

func TestDormantFeaturesMCPRefusesArguments(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	res, err := s.handleAutonomousDormantFeatures(context.Background(), callReq(map[string]any{"mode": "on"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v res=%+v", err, res)
	}
	if called {
		t.Error("a refused call must not reach the daemon")
	}
	if !strings.Contains(automationText(t, res), "mode") {
		t.Errorf("error does not name the argument: %s", automationText(t, res))
	}
}
