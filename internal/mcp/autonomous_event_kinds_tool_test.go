// MCP tool tests for autonomous_event_kinds: the handler proxies the read-only
// REST route to an httptest server standing in for the daemon, and once
// through the real daemon handler.
package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	dwserver "github.com/dmz006/datawatch/internal/server"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// eventKindsTestPort returns the port of an httptest server for
// &Server{webPort: ...}.
func eventKindsTestPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL %q: %v", srv.URL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port %q: %v", u.Port(), err)
	}
	return port
}

func TestEventKindsToolDefinition(t *testing.T) {
	tool := (&Server{}).toolAutonomousEventKinds()
	if tool.Name != "autonomous_event_kinds" {
		t.Fatalf("name = %q", tool.Name)
	}
	for _, want := range []string{"/api/autonomous/event-kinds", "Read-only", "wake_default", "autonomous_subscribe"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description lacks %q: %s", want, tool.Description)
		}
	}
	if len(tool.InputSchema.Required) != 0 || len(tool.InputSchema.Properties) != 0 {
		t.Errorf("tool takes no arguments, got required=%v properties=%v",
			tool.InputSchema.Required, tool.InputSchema.Properties)
	}
}

func TestEventKindsToolProxiesREST(t *testing.T) {
	var gotMethod, gotPath, gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotRaw = r.Method, r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groups":[{"group":"task","kinds":[{"kind":"task.failed","description":"d","wake_default":true}]}]}`))
	}))
	defer srv.Close()

	s := &Server{webPort: eventKindsTestPort(t, srv)}
	res, err := s.handleAutonomousEventKinds(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/autonomous/event-kinds" || gotRaw != "" {
		t.Errorf("request = %s %s?%s", gotMethod, gotPath, gotRaw)
	}
	if out := resContentText(t, res); !strings.Contains(out, `"task.failed"`) {
		t.Errorf("result = %s", out)
	}
}

func TestEventKindsToolReportsDaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := &Server{webPort: eventKindsTestPort(t, srv)}
	res, err := s.handleAutonomousEventKinds(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !res.IsError {
		t.Errorf("want tool error on HTTP 500, got %v", res.Content)
	}
}

func TestEventKindsToolNoLoopbackPort(t *testing.T) {
	s := &Server{webPort: 0}
	res, err := s.handleAutonomousEventKinds(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !res.IsError {
		t.Errorf("want tool error without a loopback port, got %v", res.Content)
	}
}

// TestEventKindsToolRealHandler drives the tool through the real daemon
// endpoint and checks it lists every registered kind, including one an
// extension registered.
func TestEventKindsToolRealHandler(t *testing.T) {
	err := autonomous.RegisterEventKind(autonomous.EventKindInfo{
		Kind: "ext.mcp_listing_probe", Description: "A probe kind.", Group: autonomous.EventGroupTask,
	})
	if err != nil {
		t.Fatal(err)
	}
	api := dwserver.NewServer(dwserver.NewHub(), nil, "h", "", nil, config.DefaultConfig(), filepath.Join(t.TempDir(), "config.yaml"))
	ts := httptest.NewServer(api.AutonomousEventKindsHandler())
	defer ts.Close()

	s := &Server{webPort: eventKindsTestPort(t, ts)}
	res, err := s.handleAutonomousEventKinds(t.Context(), mcpsdk.CallToolRequest{})
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	var body struct {
		Groups []struct {
			Group string `json:"group"`
			Kinds []struct {
				Kind        string `json:"kind"`
				Description string `json:"description"`
				WakeDefault bool   `json:"wake_default"`
			} `json:"kinds"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(resContentText(t, res)), &body); err != nil {
		t.Fatalf("tool did not proxy the handler JSON: %v", err)
	}
	seen := map[string]bool{}
	wake := map[string]bool{}
	for _, g := range body.Groups {
		for _, k := range g.Kinds {
			seen[k.Kind] = true
			wake[k.Kind] = k.WakeDefault
		}
	}
	for _, info := range autonomous.ListEventKinds() {
		if !seen[info.Kind] {
			t.Errorf("kind %q is registered but missing from the tool result", info.Kind)
		}
	}
	if !seen["ext.mcp_listing_probe"] || wake["ext.mcp_listing_probe"] {
		t.Errorf("extension kind missing or marked wake default: seen=%v wake=%v", seen["ext.mcp_listing_probe"], wake["ext.mcp_listing_probe"])
	}
	if !wake["task.failed"] {
		t.Errorf("task.failed should be a wake default")
	}
}
