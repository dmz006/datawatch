package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestLaneUtilizationToolIsListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range tools {
		if tl.Name == "autonomous_lane_utilization" {
			found = true
		}
	}
	if !found {
		t.Error("autonomous_lane_utilization is not registered")
	}
}

func TestLaneUtilizationToolSchema(t *testing.T) {
	tool := (&Server{}).toolAutonomousLaneUtilization()
	for _, k := range []string{"window", "bucket", "decisions"} {
		if _, ok := tool.InputSchema.Properties[k]; !ok {
			t.Errorf("schema lacks %s", k)
		}
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("all arguments must be optional, got required %v", tool.InputSchema.Required)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("tool must be marked read-only")
	}
}

func TestLaneUtilizationPassesArgumentsThroughAndReturnsBodyUnchanged(t *testing.T) {
	const body = `{"window":"7d","buckets":[],"lane_fill":{"decisions":[]}}`
	var path, method string
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.Query()
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handleAutonomousLaneUtilization(context.Background(), callReq(map[string]any{
		"window": "7d", "bucket": float64(12), "decisions": "5",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/lane-utilization" {
		t.Errorf("proxied %s %s", method, path)
	}
	for k, want := range map[string]string{"window": "7d", "bucket": "12", "decisions": "5"} {
		if got := query[k]; len(got) != 1 || got[0] != want {
			t.Errorf("%s query = %v, want [%s]", k, got, want)
		}
	}
	if text := automationText(t, res); text != body {
		t.Errorf("body changed: %s", text)
	}
}

func TestLaneUtilizationSendsNothingWhenArgumentsAbsent(t *testing.T) {
	var rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousLaneUtilization(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" {
		t.Errorf("query sent without arguments: %q", rawQuery)
	}
}

func TestLaneUtilizationBadArgumentsComeBackAsToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request: window must be a duration such as 6h, 24h or 7d", http.StatusBadRequest)
	})
	res, err := s.handleAutonomousLaneUtilization(context.Background(), callReq(map[string]any{"window": "soon"}))
	if err != nil {
		t.Fatalf("a refusal must be a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 400 must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, "window must be a duration") {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestLaneUtilizationIgnoresWrongTypedArguments(t *testing.T) {
	var rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	})
	res, err := s.handleAutonomousLaneUtilization(context.Background(), callReq(map[string]any{
		"window": true, "bucket": []any{1}, "decisions": map[string]any{},
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rawQuery != "" {
		t.Errorf("wrong-typed arguments leaked into the query: %q", rawQuery)
	}
}

func TestLaneUtilizationUpstreamErrorIsToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
	})
	res, err := s.handleAutonomousLaneUtilization(context.Background(), callReq(nil))
	if err != nil {
		t.Fatalf("an upstream error must be a tool result: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 503 must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, "autonomous disabled") {
		t.Errorf("upstream text lost: %s", text)
	}
}

func TestLaneUtilizationCapabilityMapping(t *testing.T) {
	if got := federation.MCPToolCap["autonomous_lane_utilization"]; got != federation.CapAutonomousRead {
		t.Errorf("autonomous_lane_utilization = %q, want %q", got, federation.CapAutonomousRead)
	}
}
