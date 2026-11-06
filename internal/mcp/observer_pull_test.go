// Observer pull protocol (pull mode) — MCP surface tests for the
// observer_pull_status tool.

package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestObserverPullStatus_ToolNameAndDescription(t *testing.T) {
	s := &Server{}
	tool := s.toolObserverPullStatus()
	if tool.Name != "observer_pull_status" {
		t.Errorf("tool name = %q, want observer_pull_status", tool.Name)
	}
	if tool.Description == "" {
		t.Error("tool has no description")
	}
}

func TestObserverPullStatus_HandlerProxiesEndpoint(t *testing.T) {
	rows := `{"peers":[{"name":"peer1","source":"pull","shadow":true,"stale":false,"stale_reason":"none","last_success_at":"2026-01-01T00:00:00Z","interval_s":30,"timeout_s":5}]}`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rows))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleObserverPullStatus(t.Context(), req)
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotPath != "/api/observer/pull" {
		t.Errorf("upstream path = %q, want /api/observer/pull", gotPath)
	}
	if len(res.Content) == 0 {
		t.Fatalf("empty tool result content")
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("first content is %T, not TextContent", res.Content[0])
	}
	if tc.Text != rows {
		t.Errorf("result text = %q, want the proxied body %q", tc.Text, rows)
	}
}

func TestObserverPullStatus_NoLoopbackReturnsError(t *testing.T) {
	s := &Server{} // webPort == 0 — MCP-only test mode, loopback unavailable
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleObserverPullStatus(t.Context(), req)
	if err == nil && (res == nil || !res.IsError) {
		t.Errorf("expected an error (or error result) with loopback unavailable, got res=%+v err=%v", res, err)
	}
}
