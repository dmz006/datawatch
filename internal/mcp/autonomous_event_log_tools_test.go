// MCP tool tests for autonomous_event_log: the handler proxies the read-only
// REST route to an httptest server standing in for the daemon.
package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	dwserver "github.com/dmz006/datawatch/internal/server"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestAutonomousEventLogToolDefinition(t *testing.T) {
	tool := (&Server{}).toolAutonomousEventLog()
	if tool.Name != "autonomous_event_log" {
		t.Fatalf("name = %q", tool.Name)
	}
	if !strings.Contains(tool.Description, "/api/autonomous/event-log") {
		t.Errorf("description does not name the proxied route: %s", tool.Description)
	}
	if !strings.Contains(tool.Description, "Read-only") {
		t.Errorf("description does not state the tool is read-only: %s", tool.Description)
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("required = %v, want none (every filter is optional)", tool.InputSchema.Required)
	}
	for _, key := range []string{"kind", "project", "q", "limit", "before"} {
		if _, ok := tool.InputSchema.Properties[key]; !ok {
			t.Errorf("input schema lacks %q", key)
		}
	}
}

func TestAutonomousEventLogToolProxiesFilters(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[{"kind":"prd_failed"}],"before":"c-7"}`))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"kind":    " prd_failed ",
		"project": "datawatch",
		"q":       "timeout",
		"limit":   float64(25),
		"before":  "c-9",
	}
	res, err := s.handleAutonomousEventLog(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if gotPath != "/api/autonomous/event-log" {
		t.Errorf("path = %q", gotPath)
	}
	want := map[string]string{"kind": "prd_failed", "project": "datawatch", "q": "timeout", "limit": "25", "before": "c-9"}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query %s = %q, want %q (full query %v)", k, got, v, gotQuery)
		}
	}
	if out := resContentText(t, res); !strings.Contains(out, `"before":"c-7"`) {
		t.Errorf("result = %s", out)
	}
}

func TestAutonomousEventLogToolDefaultsOmitOptionalArgs(t *testing.T) {
	var gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRaw = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"events":[]}`))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"kind": "   ", "project": ""}
	res, err := s.handleAutonomousEventLog(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if gotRaw != "" {
		t.Errorf("query = %q, want empty (blank filters are dropped)", gotRaw)
	}
}

func TestAutonomousEventLogToolRejectsBadLimit(t *testing.T) {
	s := &Server{webPort: 1}
	for _, limit := range []float64{0, -3, 2.5} {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = map[string]any{"limit": limit}
		res, err := s.handleAutonomousEventLog(t.Context(), req)
		if err != nil {
			t.Fatalf("limit %v: unexpected error %v", limit, err)
		}
		if !res.IsError {
			t.Errorf("limit %v: want tool error, got %v", limit, res.Content)
		}
	}
}

func TestAutonomousEventLogToolReportsDaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	res, err := s.handleAutonomousEventLog(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !res.IsError {
		t.Errorf("want tool error on HTTP 500, got %v", res.Content)
	}
}

func TestAutonomousEventLogToolNoLoopbackPort(t *testing.T) {
	s := &Server{webPort: 0}
	res, err := s.handleAutonomousEventLog(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !res.IsError {
		t.Errorf("want tool error without a loopback port, got %v", res.Content)
	}
}

// TestAutonomousEventLogToolRealHandler drives the tool through the real
// daemon endpoint (not a stub): a real Automata manager with a failed task
// behind the actual GET /api/autonomous/event-log handler, served on a live
// loopback port, with the MCP tool proxying it.
func TestAutonomousEventLogToolRealHandler(t *testing.T) {
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	projectDir := t.TempDir()
	prd, err := mgr.CreatePRD("spec", projectDir, "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	failedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{{
		ID: "s1", Title: "Story",
		Tasks: []autonomous.Task{
			{ID: "t-infra", Title: "infra fail", Status: autonomous.TaskFailed,
				FailureReason: &autonomous.FailureReason{
					Category: "infra", Detail: "verifier session timed out after 30m0s",
					Source: "verifier", OccurredAt: failedAt,
				}},
		},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}

	api := dwserver.NewServer(dwserver.NewHub(), nil, "h", "", nil, config.DefaultConfig(), filepath.Join(t.TempDir(), "config.yaml"))
	api.SetAutonomousAPI(autonomous.NewAPI(mgr))
	ts := httptest.NewServer(api.AutomataEventLogHandler())

	s := &Server{webPort: bl427Port(t, ts)}

	// kind filter, limit pass-through, and the classified error entry.
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"kind": "error", "limit": float64(100)}
	res, err := s.handleAutonomousEventLog(t.Context(), req)
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	var body struct {
		Events []map[string]any `json:"events"`
		Total  int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(resContentText(t, res)), &body); err != nil {
		t.Fatalf("tool did not proxy the handler JSON: %v", err)
	}
	if body.Total != 1 || len(body.Events) != 1 {
		t.Fatalf("want exactly 1 error event from the real handler, total=%d events=%v", body.Total, body.Events)
	}
	ev := body.Events[0]
	if ev["kind"] != "error" || ev["project"] != projectDir {
		t.Fatalf("event fields wrong: %v", ev)
	}
	if id, _ := ev["id"].(string); !strings.HasPrefix(id, "error:verifier:") {
		t.Fatalf("id = %q, want an error:verifier: id", id)
	}
	if ev["summary"] == "" || ev["next_action"] == "" {
		t.Fatalf("classified entry needs a summary and a next action: %v", ev)
	}

	// The daemon's own validation still applies through the proxy: an unknown
	// kind is a 400 the tool reports as a tool error.
	req.Params.Arguments = map[string]any{"kind": "bogus_kind"}
	res, err = s.handleAutonomousEventLog(t.Context(), req)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !res.IsError {
		t.Fatalf("want tool error on the handler's 400, got %v", res.Content)
	}
}
