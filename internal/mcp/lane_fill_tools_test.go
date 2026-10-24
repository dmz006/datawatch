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

func TestLaneFillToolsAreListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tl := range tools {
		have[tl.Name] = true
	}
	for _, name := range []string{"autonomous_lane_fill_status", "autonomous_prd_set_lane_preference"} {
		if !have[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
}

func TestLaneFillToolSchemas(t *testing.T) {
	s := &Server{}
	status := s.toolAutonomousLaneFillStatus()
	if status.Name != "autonomous_lane_fill_status" {
		t.Fatalf("name = %q", status.Name)
	}
	if _, ok := status.InputSchema.Properties["limit"]; !ok {
		t.Error("autonomous_lane_fill_status lacks the limit parameter")
	}
	if len(status.InputSchema.Required) != 0 {
		t.Errorf("limit must be optional, got required %v", status.InputSchema.Required)
	}
	if status.Annotations.ReadOnlyHint == nil || !*status.Annotations.ReadOnlyHint {
		t.Error("autonomous_lane_fill_status must be marked read-only")
	}

	pref := s.toolAutonomousPRDSetLanePreference()
	if pref.Name != "autonomous_prd_set_lane_preference" {
		t.Fatalf("name = %q", pref.Name)
	}
	required := strings.Join(pref.InputSchema.Required, ",")
	if !strings.Contains(required, "id") || !strings.Contains(required, "lane_preference") {
		t.Errorf("id and lane_preference must be required, got %q", required)
	}
	if _, ok := pref.InputSchema.Properties["lane_preference"]; !ok {
		t.Error("autonomous_prd_set_lane_preference lacks lane_preference")
	}
}

func TestLaneFillStatusProxiesGETAndPassesLimitThrough(t *testing.T) {
	var path, method string
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.Query()
		_, _ = w.Write([]byte(`{"lanes":[]}`))
	})
	res, err := s.handleAutonomousLaneFillStatus(context.Background(), callReq(map[string]any{"limit": float64(25)}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/lane-fill" {
		t.Errorf("proxied %s %s", method, path)
	}
	if got := query["limit"]; len(got) != 1 || got[0] != "25" {
		t.Errorf("limit query = %v, want [25]", got)
	}
	if text := automationText(t, res); !strings.Contains(text, `"lanes"`) {
		t.Errorf("result lost the lane fill body: %s", text)
	}
}

func TestLaneFillStatusAcceptsLimitAsText(t *testing.T) {
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousLaneFillStatus(context.Background(), callReq(map[string]any{"limit": "7"})); err != nil {
		t.Fatal(err)
	}
	if got := query["limit"]; len(got) != 1 || got[0] != "7" {
		t.Errorf("limit query = %v, want [7]", got)
	}
}

func TestLaneFillStatusSendsNoLimitWhenAbsent(t *testing.T) {
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousLaneFillStatus(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if _, ok := query["limit"]; ok {
		t.Errorf("limit sent without the argument: %v", query)
	}
}

func TestLaneFillStatusSurfacesValidationError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "limit must be a whole number from 1 to 1000", http.StatusBadRequest)
	})
	res, err := s.handleAutonomousLaneFillStatus(context.Background(), callReq(map[string]any{"limit": float64(0)}))
	if err != nil {
		t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, "limit must be a whole number") {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestLaneFillSetLanePreferenceProxiesPOSTBody(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"p1","lane_preference":"gpu"}`)
	res, err := s.handleAutonomousPRDSetLanePreference(context.Background(), callReq(map[string]any{
		"id": "p1", "lane_preference": "gpu",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/prds/p1/set_lane_preference" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["lane_preference"] != "gpu" {
		t.Errorf("body = %v, want lane_preference gpu", rec.body)
	}
}

func TestLaneFillSetLanePreferenceEmptyMeansAny(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousPRDSetLanePreference(context.Background(), callReq(map[string]any{
		"id": "p1", "lane_preference": "",
	})); err != nil {
		t.Fatal(err)
	}
	v, ok := rec.body["lane_preference"]
	if !ok || v != "" {
		t.Errorf("body = %v, want an explicit empty lane_preference", rec.body)
	}
}

func TestLaneFillSetLanePreferenceSurfacesRefusal(t *testing.T) {
	s, _ := activateServer(t, http.StatusBadRequest, "unknown lane preference \"tpu\": use gpu, claude or any")
	res, err := s.handleAutonomousPRDSetLanePreference(context.Background(), callReq(map[string]any{
		"id": "p1", "lane_preference": "tpu",
	}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a refused preference must be a tool error: err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, "use gpu, claude or any") {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestLaneFillConfigSetNestsLaneFillObject(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"lane_fill_mode":                "on",
		"lane_fill_gpu_lanes":           float64(0),
		"lane_fill_claude_lanes":        float64(4),
		"lane_fill_idle_alert_minutes":  float64(15),
		"lane_fill_decompose_ahead_min": float64(3),
		"lane_fill_poll_seconds":        float64(30),
		"lane_fill_gpu_backend":         "pi-prompt",
		"lane_fill_gpu_model":           "qwen3.8:27b",
		"lane_fill_claude_backend":      "claude-code",
	})); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/autonomous/config" {
		t.Fatalf("proxied %s %s, want PUT /api/autonomous/config", rec.method, rec.path)
	}
	lane, _ := rec.body["lane_fill"].(map[string]any)
	want := map[string]any{
		"mode":                "on",
		"gpu_lanes":           float64(0),
		"claude_lanes":        float64(4),
		"idle_alert_minutes":  float64(15),
		"decompose_ahead_min": float64(3),
		"poll_seconds":        float64(30),
		"gpu_backend":         "pi-prompt",
		"gpu_model":           "qwen3.8:27b",
		"claude_backend":      "claude-code",
	}
	if len(lane) != len(want) {
		t.Errorf("lane_fill = %v, want %d keys", lane, len(want))
	}
	for k, v := range want {
		if got, ok := lane[k]; !ok || got != v {
			t.Errorf("lane_fill[%s] = %v (present=%v), want %v", k, got, ok, v)
		}
	}
}

func TestLaneFillConfigSetOmitsLaneFillWhenAbsent(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"poll_interval_seconds": 5})); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.body["lane_fill"]; ok {
		t.Errorf("lane_fill key sent without any lane_fill argument: %v", rec.body)
	}
}

func TestLaneFillConfigSetSurfacesRejectedValue(t *testing.T) {
	s, _ := activateServer(t, http.StatusBadRequest, "autonomous.lane_fill.mode: \"fast\" is not a valid value (must be \"off\", \"shadow\" or \"on\")")
	// The config set handler returns the daemon's refusal as the handler error,
	// the same as every other autonomous config argument.
	_, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"lane_fill_mode": "fast"}))
	if err == nil || !strings.Contains(err.Error(), "autonomous.lane_fill.mode") {
		t.Fatalf("a 400 from the daemon must be surfaced with its text, got err=%v", err)
	}
}

func TestLaneFillConfigSetDeclaresEveryArgument(t *testing.T) {
	names := configSetArgNames()
	for _, k := range []string{
		"lane_fill_mode", "lane_fill_gpu_lanes", "lane_fill_claude_lanes", "lane_fill_idle_alert_minutes",
		"lane_fill_decompose_ahead_min", "lane_fill_poll_seconds", "lane_fill_gpu_backend",
		"lane_fill_gpu_model", "lane_fill_claude_backend",
	} {
		if !strings.Contains(names, k) {
			t.Errorf("autonomous_config_set does not declare %s", k)
		}
	}
}

func TestLaneFillCapabilityMapping(t *testing.T) {
	if got := federation.MCPToolCap["autonomous_lane_fill_status"]; got != federation.CapAutonomousRead {
		t.Errorf("autonomous_lane_fill_status = %q, want %q", got, federation.CapAutonomousRead)
	}
	if got := federation.MCPToolCap["autonomous_prd_set_lane_preference"]; got != federation.CapAutonomousWrite {
		t.Errorf("autonomous_prd_set_lane_preference = %q, want %q", got, federation.CapAutonomousWrite)
	}
}
