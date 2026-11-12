package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const dashboardArcMCPFixture = `{"generated_at":"2026-11-01T09:00:00Z","available":true,"arc":{"available":true,"id":"a1","name":"Release arc","stages":[{"stage":"bugs","total":4,"done":2,"running":1,"blocked":0,"percent":50},{"stage":"features","total":2,"done":0,"running":0,"blocked":1,"percent":0},{"stage":"final","total":1,"done":0,"running":0,"blocked":0,"percent":0}],"maintenance_queue":{"Total":3,"Done":1,"Running":0,"Blocked":0},"blocked_stage":{"stage":"features","reason":"Login is blocked: waiting on review"}},"lanes":{"available":true,"lanes":[{"kind":"claude","in_flight":1,"cap":2}],"queued_by_priority":[]},"usage":{"available":true,"points":{"used":12.5,"allowance":100,"remaining":87.5,"percent":12.5},"tokens":{"in":1000,"out":500,"total":1500}}}`

func TestToolDashboardArcGetDescriptor(t *testing.T) {
	tool := (&Server{}).toolDashboardArcGet()
	if tool.Name != "dashboard_arc_get" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if !strings.Contains(tool.Description, "GET /api/dashboard/arc") {
		t.Errorf("description = %q", tool.Description)
	}
}

func TestHandleDashboardArcGetMCPProxiesArc(t *testing.T) {
	var gotPath, gotArc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotArc = r.URL.Path, r.URL.Query().Get("arc")
		_, _ = w.Write([]byte(dashboardArcMCPFixture))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"arc": "a1"}
	res, err := s.handleDashboardArcGetMCP(t.Context(), req)
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotPath != "/api/dashboard/arc" || gotArc != "a1" {
		t.Errorf("path=%q arc=%q", gotPath, gotArc)
	}
	out := resContentText(t, res)
	for _, want := range []string{"Release arc", "points", "tokens"} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q", want)
		}
	}
	if strings.Contains(out, "$") {
		t.Errorf("result must not show dollars: %s", out)
	}
}

func TestHandleDashboardArcGetMCPRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "disabled", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	if _, err := s.handleDashboardArcGetMCP(t.Context(), mcpsdk.CallToolRequest{}); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("want 503 error, got %v", err)
	}
}
