// MCP tool tests for autonomous_events_pending: the handler proxies the
// REST route to an httptest server standing in for the daemon.
package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestPendingEventsToolDefinition(t *testing.T) {
	tool := (&Server{}).toolAutonomousEventsPending()
	if tool.Name != "autonomous_events_pending" {
		t.Fatalf("name = %q", tool.Name)
	}
	for _, want := range []string{"next_cursor", "since_cursor", "mcp_queue", "autonomous_subscribe"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description lacks %q: %s", want, tool.Description)
		}
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "queue" {
		t.Errorf("required = %v, want [queue]", tool.InputSchema.Required)
	}
	sub := (&Server{}).toolAutonomousSubscribe()
	if !strings.Contains(sub.Description, "mcp_queue") {
		t.Errorf("autonomous_subscribe description does not document mcp_queue")
	}
	props, _ := sub.InputSchema.Properties["sink_type"].(map[string]any)
	if d, _ := props["description"].(string); !strings.Contains(d, "mcp_queue") {
		t.Errorf("sink_type description does not document mcp_queue: %v", props)
	}
}

func TestPendingEventsToolProxiesREST(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[],"next_cursor":9,"dropped":false}`))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"queue": "ops:1", "since_cursor": float64(4), "limit": float64(20)}
	res, err := s.handleAutonomousEventsPending(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if gotPath != "/api/autonomous/events" {
		t.Errorf("path = %q", gotPath)
	}
	for _, want := range []string{"queue=ops%3A1", "since=4", "limit=20"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q lacks %q", gotQuery, want)
		}
	}
	if out := resContentText(t, res); !strings.Contains(out, `"next_cursor":9`) {
		t.Errorf("result = %s", out)
	}
}

func TestPendingEventsToolDefaultsOmitOptionalArgs(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"events":[],"next_cursor":0,"dropped":false}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"queue": "q"}
	if res, _ := s.handleAutonomousEventsPending(t.Context(), req); res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	if gotQuery != "queue=q" {
		t.Errorf("query = %q, want queue=q", gotQuery)
	}
}

func TestPendingEventsToolValidatesArgs(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	cases := []map[string]any{
		{},
		{"queue": "   "},
		{"queue": "q", "since_cursor": float64(-1)},
		{"queue": "q", "since_cursor": 1.5},
		{"queue": "q", "limit": float64(0)},
		{"queue": "q", "limit": 2.5},
	}
	for _, args := range cases {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleAutonomousEventsPending(t.Context(), req)
		if err != nil || !res.IsError {
			t.Errorf("args %v: want a tool error, got err=%v res=%v", args, err, res)
		}
	}
	if called {
		t.Error("an invalid call reached the daemon")
	}
}

func TestPendingEventsToolSurfacesDaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "pending events queue disabled", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"queue": "q"}
	res, _ := s.handleAutonomousEventsPending(t.Context(), req)
	if !res.IsError || !strings.Contains(resContentText(t, res), "503") {
		t.Fatalf("want a 503 tool error, got %v", res)
	}
}
