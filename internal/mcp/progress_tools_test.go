package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestProgressToolsRegistered(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
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
	for _, name := range []string{"autonomous_progress", "autonomous_progress_trend"} {
		if !have[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
}

func TestProgressToolsCapabilityMapping(t *testing.T) {
	for _, name := range []string{"autonomous_progress", "autonomous_progress_trend"} {
		got, ok := federation.MCPToolCap[name]
		if !ok || got != federation.CapAutonomousRead {
			t.Errorf("MCPToolCap[%q] = %q (present=%v), want %q", name, got, ok, federation.CapAutonomousRead)
		}
	}
}

func TestProgressToolShapes(t *testing.T) {
	s := &Server{}
	p := s.toolAutonomousProgress()
	if p.Name != "autonomous_progress" {
		t.Fatalf("name = %q", p.Name)
	}
	for _, k := range []string{"automation_id", "tag_id", "since", "until"} {
		if _, ok := p.InputSchema.Properties[k]; !ok {
			t.Errorf("autonomous_progress lacks the %s parameter", k)
		}
	}
	if len(p.InputSchema.Required) != 0 {
		t.Errorf("every autonomous_progress parameter is optional, got required %v", p.InputSchema.Required)
	}
	tr := s.toolAutonomousProgressTrend()
	if tr.Name != "autonomous_progress_trend" {
		t.Fatalf("name = %q", tr.Name)
	}
	if _, ok := tr.InputSchema.Properties["days"]; !ok || len(tr.InputSchema.Required) != 0 {
		t.Errorf("autonomous_progress_trend takes one optional days parameter: %+v", tr.InputSchema)
	}
	for _, tool := range []mcpsdk.Tool{p, tr} {
		if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must be marked read-only", tool.Name)
		}
		if strings.Contains(tool.Description, "PRD") {
			t.Errorf("%s description uses the internal term", tool.Name)
		}
	}
}

func progressResultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil || res.IsError || len(res.Content) == 0 {
		t.Fatalf("bad result: %+v", res)
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("not text content: %T", res.Content[0])
	}
	return tc.Text
}

// The tool returns the body of the REST route unchanged and forwards its
// parameters as the query.
func TestProgressToolProxiesRESTBody(t *testing.T) {
	const body = `{"scope":"tag:t1","groups":{},"backlog_available":false}`
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handleAutonomousProgress(context.Background(), callReq(map[string]any{
		"automation_id": "a1", "tag_id": "t1", "since": "2026-10-10", "until": "2026-10-20",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := progressResultText(t, res); got != body {
		t.Fatalf("body differs from the REST response:\n got: %s\nwant: %s", got, body)
	}
	if method != http.MethodGet || path != "/api/autonomous/progress" {
		t.Fatalf("proxied %s %s", method, path)
	}
	for _, want := range []string{"automation_id=a1", "tag_id=t1", "since=2026-10-10", "until=2026-10-20"} {
		if !strings.Contains(rawQuery, want) {
			t.Errorf("query %q lacks %s", rawQuery, want)
		}
	}

	// No arguments, no query.
	if _, err := s.handleAutonomousProgress(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" {
		t.Fatalf("no parameters must send no query, got %q", rawQuery)
	}
}

func TestProgressTrendToolProxiesRESTBody(t *testing.T) {
	const body = `{"days":7,"points":[],"direction":"unknown"}`
	var path, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, rawQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})
	for _, days := range []any{float64(7), "7"} {
		res, err := s.handleAutonomousProgressTrend(context.Background(), callReq(map[string]any{"days": days}))
		if err != nil {
			t.Fatal(err)
		}
		if got := progressResultText(t, res); got != body {
			t.Fatalf("body differs from the REST response: %s", got)
		}
		if path != "/api/autonomous/progress/trend" || rawQuery != "days=7" {
			t.Fatalf("proxied %s?%s", path, rawQuery)
		}
	}
	if _, err := s.handleAutonomousProgressTrend(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" {
		t.Fatalf("no days must send no query, got %q", rawQuery)
	}
}

func TestProgressToolsSurfaceRESTRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request: days: must be a whole number from 1 to 365", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}
	if res, err := s.handleAutonomousProgressTrend(context.Background(), callReq(map[string]any{"days": float64(9999)})); err == nil && (res == nil || !res.IsError) {
		t.Fatalf("a 400 from the daemon must not come back as success: res=%+v", res)
	}
}

func TestProgressToolsWithoutLoopback(t *testing.T) {
	s := &Server{}
	if _, err := s.handleAutonomousProgress(context.Background(), callReq(nil)); err == nil {
		t.Fatal("want an error when the REST loopback is unavailable")
	}
}
