package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const dashboardConsoleMCPFixture = `{
	"generated_at": "2026-11-01T09:00:00Z",
	"sections": {
		"questions_waiting": {"available": true, "count": 2, "summary": "2 sessions waiting on questions", "link": "/console?section=questions"},
		"held_blocked": {"available": true, "count": 0, "summary": "no held or blocked sessions", "link": "/console?section=held"},
		"lanes": {"available": false, "count": 0, "summary": "lane fill is off", "link": "/lanes"},
		"usage": {"available": true, "count": 5, "summary": "$12.50 spent today", "link": "/usage"},
		"ci_release": {"available": true, "count": 1, "summary": "1 CI red on main", "link": "/ci-release"}
	}
}`

func TestToolDashboardConsoleGetDescriptor(t *testing.T) {
	tool := (&Server{}).toolDashboardConsoleGet()
	if tool.Name != "dashboard_console_get" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if !strings.Contains(tool.Description, "GET /api/dashboard/console") {
		t.Errorf("description = %q", tool.Description)
	}
}

func TestHandleDashboardConsoleGetMCPProxiesConsole(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dashboardConsoleMCPFixture))
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	res, err := s.handleDashboardConsoleGetMCP(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotPath != "/api/dashboard/console" {
		t.Errorf("path = %q", gotPath)
	}
	out := resContentText(t, res)
	for _, want := range []string{"questions_waiting", "held_blocked", "lanes", "usage", "ci_release", "$12.50 spent today"} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
}

func TestHandleDashboardConsoleGetMCPDisabledSurfacesRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the operator console is disabled — set dashboard.console.enabled to true", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := &Server{webPort: bl427Port(t, srv)}
	res, err := s.handleDashboardConsoleGetMCP(t.Context(), mcpsdk.CallToolRequest{})
	if err == nil {
		t.Fatalf("want a 503 error, res=%+v", res)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error lacks 503: %v", err)
	}
	if !strings.Contains(err.Error(), "dashboard.console.enabled") {
		t.Errorf("error does not name the key to flip: %v", err)
	}
}
