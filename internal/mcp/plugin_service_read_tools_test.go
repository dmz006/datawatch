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

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const pluginServiceReadNames = "plugin_service_status, plugin_widget_list, plugin_widget_get, plugin_token_list"

func TestPluginServiceReadToolsAreListed(t *testing.T) {
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
	for _, name := range strings.Split(pluginServiceReadNames, ", ") {
		if !have[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
}

// TestPluginServiceReadToolSchemas asserts the argument shapes and the
// read-only hint on every one of the four new tools.
func TestPluginServiceReadToolSchemas(t *testing.T) {
	s := &Server{}
	cases := []struct {
		name     string
		tool     mcpsdk.Tool
		required []string
	}{
		{"plugin_service_status", s.toolPluginServiceStatus(), []string{"name"}},
		{"plugin_widget_list", s.toolPluginWidgetList(), []string{"name"}},
		{"plugin_widget_get", s.toolPluginWidgetGet(), []string{"name", "widget_id"}},
		{"plugin_token_list", s.toolPluginTokenList(), []string{"name"}},
	}
	for _, tc := range cases {
		if tc.tool.Name != tc.name {
			t.Fatalf("tool name = %q, want %q", tc.tool.Name, tc.name)
		}
		gotRequired := map[string]bool{}
		for _, r := range tc.tool.InputSchema.Required {
			gotRequired[r] = true
		}
		for _, want := range tc.required {
			if !gotRequired[want] {
				t.Errorf("%s: parameter %q must be required (required=%v)", tc.name, want, tc.tool.InputSchema.Required)
			}
			if _, ok := tc.tool.InputSchema.Properties[want]; !ok {
				t.Errorf("%s: parameter %q missing from input schema", tc.name, want)
			}
		}
		if len(tc.tool.InputSchema.Required) != len(tc.required) {
			t.Errorf("%s: required = %v, want exactly %v", tc.name, tc.tool.InputSchema.Required, tc.required)
		}
		if tc.tool.Annotations.ReadOnlyHint == nil || !*tc.tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: must carry the read-only hint", tc.name)
		}
	}
}

func newPluginServiceReadServer(t *testing.T, srv *httptest.Server) *Server {
	t.Helper()
	part := strings.Split(srv.URL, ":")
	if len(part) < 3 {
		t.Fatalf("parse test server URL %q", srv.URL)
	}
	port, err := strconv.Atoi(part[2])
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return &Server{webPort: port}
}

func TestPluginServiceStatus_ProxiesPluginGet(t *testing.T) {
	body := `{"name":"svc","kind":"service","service":{"mode":"managed","state":"healthy","address":"http://127.0.0.1:9","version":"1.2.3","uptime_seconds":42,"last_health_check":"2026-11-01T01:00:00Z"}}`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	s := newPluginServiceReadServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc"}
	res, err := s.handlePluginServiceStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginServiceStatus: %v", err)
	}
	if gotPath != "/api/plugins/svc" {
		t.Errorf("path = %q, want /api/plugins/svc", gotPath)
	}
	if res == nil || len(res.Content) == 0 || res.Content[0].(mcpsdk.TextContent).Text != body {
		t.Errorf("content does not echo the REST body verbatim: %v", res)
	}
}

func TestPluginWidgetList_ProxiesWidgetsEndpoint(t *testing.T) {
	body := `{"plugin":"svc","widgets":[{"id":"cpu","value":0.42}]}`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	s := newPluginServiceReadServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc"}
	res, err := s.handlePluginWidgetList(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginWidgetList: %v", err)
	}
	if gotPath != "/api/plugins/svc/widgets" {
		t.Errorf("path = %q, want /api/plugins/svc/widgets", gotPath)
	}
	if res == nil || res.Content[0].(mcpsdk.TextContent).Text != body {
		t.Errorf("content does not echo the REST body verbatim: %v", res)
	}
}

func TestPluginWidgetGet_ProxiesWidgetEndpoint(t *testing.T) {
	body := `{"id":"cpu","value":0.42}`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	s := newPluginServiceReadServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc", "widget_id": "cpu"}
	res, err := s.handlePluginWidgetGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginWidgetGet: %v", err)
	}
	if gotPath != "/api/plugins/svc/widgets/cpu" {
		t.Errorf("path = %q, want /api/plugins/svc/widgets/cpu", gotPath)
	}
	if res == nil || res.Content[0].(mcpsdk.TextContent).Text != body {
		t.Errorf("content does not echo the REST body verbatim: %v", res)
	}
}

func TestPluginTokenList_ProxiesTokensEndpoint(t *testing.T) {
	body := `{"plugin":"svc","secrets":[{"name":"api_key","fields":["connection.auth.token"],"exists":true}],"service_tokens":[{"name":"api_key","created_at":"2026-10-25T00:00:00Z"}]}`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	s := newPluginServiceReadServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc"}
	res, err := s.handlePluginTokenList(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginTokenList: %v", err)
	}
	if gotPath != "/api/plugins/svc/tokens" {
		t.Errorf("path = %q, want /api/plugins/svc/tokens", gotPath)
	}
	if res == nil || res.Content[0].(mcpsdk.TextContent).Text != body {
		t.Errorf("content does not echo the REST body verbatim: %v", res)
	}
}

// Argument validation: a missing required argument is refused before any
// HTTP call is made.
func TestPluginServiceReadTools_RefuseEmptyName(t *testing.T) {
	s := &Server{} // webPort 0 — any proxy attempt would fail loudly
	cases := []struct {
		name string
		args map[string]any
		call func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	}{
		{"plugin_service_status", map[string]any{}, s.handlePluginServiceStatus},
		{"plugin_service_status", map[string]any{"name": ""}, s.handlePluginServiceStatus},
		{"plugin_widget_list", map[string]any{}, s.handlePluginWidgetList},
		{"plugin_widget_list", map[string]any{"name": ""}, s.handlePluginWidgetList},
		{"plugin_widget_get", map[string]any{}, s.handlePluginWidgetGet},
		{"plugin_widget_get", map[string]any{"name": "svc"}, s.handlePluginWidgetGet},
		{"plugin_widget_get", map[string]any{"name": "svc", "widget_id": ""}, s.handlePluginWidgetGet},
		{"plugin_token_list", map[string]any{}, s.handlePluginTokenList},
		{"plugin_token_list", map[string]any{"name": ""}, s.handlePluginTokenList},
	}
	for _, tc := range cases {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = tc.args
		res, err := tc.call(context.Background(), req)
		if err == nil {
			t.Errorf("%s with %v: expected an error for a missing required argument, got result %+v", tc.name, tc.args, res)
		}
		if res != nil {
			t.Errorf("%s with %v: expected no result alongside an error, got %+v", tc.name, tc.args, res)
		}
	}
}

// REST 404 (unknown plugin) surfaces as a tool error, not a success.
func TestPluginServiceReadTools_UpstreamErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "plugin \"nope\" not found", http.StatusNotFound)
	}))
	defer srv.Close()
	s := newPluginServiceReadServer(t, srv)

	for _, tc := range []struct {
		name string
		args map[string]any
		call func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	}{
		{"plugin_service_status", map[string]any{"name": "nope"}, s.handlePluginServiceStatus},
		{"plugin_widget_list", map[string]any{"name": "nope"}, s.handlePluginWidgetList},
		{"plugin_widget_get", map[string]any{"name": "nope", "widget_id": "cpu"}, s.handlePluginWidgetGet},
		{"plugin_token_list", map[string]any{"name": "nope"}, s.handlePluginTokenList},
	} {
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = tc.args
		res, err := tc.call(context.Background(), req)
		if err == nil {
			t.Errorf("%s: expected an error for upstream 404, got %+v", tc.name, res)
		}
	}
}

// A cap entry exists for every new tool, and it is the plugin:read gate
// (not the generic config read used by the BL33 mutation tools).
func TestPluginServiceReadToolsHaveCapEntries(t *testing.T) {
	for _, name := range strings.Split(pluginServiceReadNames, ", ") {
		cap, ok := federation.MCPToolCap[name]
		if !ok {
			t.Errorf("tool %q has no federation.MCPToolCap entry — POST /api/mcp/call would reject it", name)
			continue
		}
		if cap != federation.CapPluginRead {
			t.Errorf("%s cap = %q, want %q", name, cap, federation.CapPluginRead)
		}
	}
}
