package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestTimelineToolsRegistered(t *testing.T) {
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
	have := map[string]bool{}
	for _, tl := range tools {
		have[tl.Name] = true
	}
	if !have["autonomous_timeline"] {
		t.Error("tool autonomous_timeline is not registered")
	}
}

func TestTimelineToolCapabilityMapping(t *testing.T) {
	got, ok := federation.MCPToolCap["autonomous_timeline"]
	if !ok || got != federation.CapAutonomousRead {
		t.Errorf("MCPToolCap[\"autonomous_timeline\"] = %q (present=%v), want %q", got, ok, federation.CapAutonomousRead)
	}
}

func TestTimelineToolShape(t *testing.T) {
	s := &Server{}
	tl := s.toolAutonomousTimeline()
	if tl.Name != "autonomous_timeline" {
		t.Fatalf("name = %q", tl.Name)
	}
	for _, k := range []string{"project", "automaton", "stage", "kind", "tag"} {
		if _, ok := tl.InputSchema.Properties[k]; !ok {
			t.Errorf("autonomous_timeline lacks the %s parameter", k)
		}
	}
	if len(tl.InputSchema.Required) != 0 {
		t.Errorf("every autonomous_timeline parameter is optional, got required %v", tl.InputSchema.Required)
	}
	if tl.Annotations.ReadOnlyHint == nil || !*tl.Annotations.ReadOnlyHint {
		t.Error("autonomous_timeline must be marked read-only")
	}
	if strings.Contains(tl.Description, "PRD") {
		t.Error("autonomous_timeline description uses the internal term")
	}
}

// The tool returns the body of the REST route unchanged and forwards its
// parameters as the query.
func TestTimelineToolProxiesRESTBody(t *testing.T) {
	const body = `{"today":"2026-10-23T12:00:00Z","projects":[],"counts":{"projects":0}}`
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})
	res, err := s.handleAutonomousTimeline(context.Background(), callReq(map[string]any{
		"project": "/home/dmz/projA", "automaton": "Fix the crash", "stage": "Wire it up", "kind": "bug", "tag": "tag1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := progressResultText(t, res); got != body {
		t.Fatalf("body differs from the REST response:\n got: %s\nwant: %s", got, body)
	}
	if method != http.MethodGet || path != "/api/autonomous/timeline" {
		t.Fatalf("proxied %s %s", method, path)
	}
	for _, want := range []string{"project=", "automaton=", "stage=", "kind=bug", "tag=tag1"} {
		if !strings.Contains(rawQuery, want) {
			t.Errorf("query %q lacks %s", rawQuery, want)
		}
	}

	// No arguments, no query.
	if _, err := s.handleAutonomousTimeline(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" {
		t.Fatalf("no parameters must send no query, got %q", rawQuery)
	}
}

func TestTimelineToolsWithoutLoopback(t *testing.T) {
	s := &Server{}
	if _, err := s.handleAutonomousTimeline(context.Background(), callReq(nil)); err == nil {
		t.Fatal("want an error when the REST loopback is unavailable")
	}
}

// A refusal from the daemon must not come back as success.
func TestTimelineToolsSurfaceRESTRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "federated peer lacks the autonomous read capability", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}
	res, err := s.handleAutonomousTimeline(context.Background(), callReq(map[string]any{"project": "/home/dmz/projA"}))
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("a 403 from the daemon must not come back as success: res=%+v", res)
	}
}
