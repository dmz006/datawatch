package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func dashDataServer(t *testing.T, gotPath, gotQuery *string, body string) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath, *gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}
}

func dashDataReq(args map[string]any) mcpsdk.CallToolRequest {
	var r mcpsdk.CallToolRequest
	r.Params.Arguments = args
	return r
}

func TestDashboardDataToolDescriptors(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]mcpsdk.Tool{
		"dashboard_work_map_get":   s.toolDashboardWorkMapGet(),
		"dashboard_lane_pulse_get": s.toolDashboardLanePulseGet(),
		"dashboard_series_get":     s.toolDashboardSeriesGet(),
	} {
		if tool.Name != name {
			t.Errorf("tool name = %q, want %q", tool.Name, name)
		}
		if !strings.Contains(tool.Description, "/api/dashboard/") {
			t.Errorf("%s description names no endpoint: %q", name, tool.Description)
		}
	}
}

func TestDashboardWorkMapGetMCPProxies(t *testing.T) {
	var p, q string
	s := dashDataServer(t, &p, &q, `{"nodes":[{"id":"a"}]}`)
	res, err := s.handleDashboardWorkMapGetMCP(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if p != "/api/dashboard/work-map" {
		t.Errorf("path = %q", p)
	}
	if out := resContentText(t, res); out != `{"nodes":[{"id":"a"}]}` {
		t.Errorf("body not verbatim: %s", out)
	}
}

func TestDashboardLanePulseGetMCPWindow(t *testing.T) {
	var p, q string
	s := dashDataServer(t, &p, &q, `{"lanes":[]}`)
	if _, err := s.handleDashboardLanePulseGetMCP(t.Context(), dashDataReq(map[string]any{"window": "12h"})); err != nil {
		t.Fatal(err)
	}
	if p != "/api/dashboard/lane-pulse" || q != "window=12h" {
		t.Errorf("got %s?%s", p, q)
	}
}

func TestDashboardSeriesGetMCPAllSix(t *testing.T) {
	for _, name := range dashboardSeriesNames {
		var p, q string
		s := dashDataServer(t, &p, &q, `{"points":[]}`)
		res, err := s.handleDashboardSeriesGetMCP(t.Context(), dashDataReq(map[string]any{"series": name, "days": float64(7)}))
		if err != nil || res.IsError {
			t.Fatalf("%s: err=%v", name, err)
		}
		if p != "/api/dashboard/series/"+name || q != "days=7" {
			t.Errorf("%s: got %s?%s", name, p, q)
		}
	}
}

func TestDashboardSeriesGetMCPRejectsUnknown(t *testing.T) {
	var p, q string
	s := dashDataServer(t, &p, &q, `{}`)
	if _, err := s.handleDashboardSeriesGetMCP(t.Context(), dashDataReq(map[string]any{"series": "../console"})); err == nil {
		t.Fatal("want an error for an unknown series")
	}
	if p != "" {
		t.Errorf("unknown series reached the daemon: %s", p)
	}
}

func TestDashboardSeriesGetMCPSurfacesRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request: days", http.StatusBadRequest)
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	_, err := s.handleDashboardSeriesGetMCP(t.Context(), dashDataReq(map[string]any{"series": "release-train", "days": float64(999)}))
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("want a 400 error, got %v", err)
	}
}
