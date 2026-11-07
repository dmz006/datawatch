// Managed-plugin lifecycle write tools (start/stop/restart) and the
// plugin token rotation tool: registration, schemas, the exact REST
// calls they proxy, the attach-only refusal message, and the
// never-echo-the-value guarantee on plugin_token_rotate.

package mcp

import (
	"context"
	"io"
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

const pluginServiceWriteNames = "plugin_service_start, plugin_service_stop, plugin_service_restart, plugin_token_rotate"

func TestPluginServiceWriteToolsAreListed(t *testing.T) {
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
	for _, name := range strings.Split(pluginServiceWriteNames, ", ") {
		if !have[name] {
			t.Errorf("tool %q is not registered", name)
		}
	}
}

// TestPluginServiceWriteToolSchemas asserts the argument shapes and
// that none of the write tools carries the read-only hint (they all
// mutate state server-side).
func TestPluginServiceWriteToolSchemas(t *testing.T) {
	s := &Server{}
	cases := []struct {
		name     string
		tool     mcpsdk.Tool
		required []string
	}{
		{"plugin_service_start", s.toolPluginServiceStart(), []string{"name"}},
		{"plugin_service_stop", s.toolPluginServiceStop(), []string{"name"}},
		{"plugin_service_restart", s.toolPluginServiceRestart(), []string{"name"}},
		{"plugin_token_rotate", s.toolPluginTokenRotate(), []string{"name", "secret", "new_value"}},
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
		if tc.tool.Annotations.ReadOnlyHint != nil && *tc.tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: must not carry the read-only hint — it mutates plugin state", tc.name)
		}
	}
}

func newPluginServiceWriteServer(t *testing.T, srv *httptest.Server) *Server {
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

// recordingClient captures each request the handler sends so tests can
// assert method, path and body.
type recordingClient struct {
	methods []string
	paths   []string
	bodies  []string
}

// TestPluginServiceStart_ProxiesEnableManagedTrue pins the "start"
// contract: POST /api/plugins/{name}/enable with the managed opt-in
// body.
func TestPluginServiceStart_ProxiesEnableManagedTrue(t *testing.T) {
	rec := &recordingClient{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.methods = append(rec.methods, r.Method)
		rec.paths = append(rec.paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		rec.bodies = append(rec.bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"enabled","managed":true,"previous_managed":false}`))
	}))
	defer srv.Close()
	s := newPluginServiceWriteServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc"}
	res, err := s.handlePluginServiceStart(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginServiceStart: %v", err)
	}
	if len(rec.paths) != 1 || rec.paths[0] != "/api/plugins/svc/enable" {
		t.Errorf("paths = %v, want [/api/plugins/svc/enable]", rec.paths)
	}
	if rec.methods[0] != http.MethodPost {
		t.Errorf("method = %q, want POST", rec.methods[0])
	}
	if rec.bodies[0] != `{"managed":true}` {
		t.Errorf("body = %q, want {\"managed\":true}", rec.bodies[0])
	}
	if res == nil || len(res.Content) == 0 || !strings.Contains(res.Content[0].(mcpsdk.TextContent).Text, `"managed":true`) {
		t.Errorf("content does not surface the REST response: %v", res)
	}
}

// TestPluginServiceStop_ProxiesEnableManagedFalse pins the "stop"
// contract: the same route with the managed opt-out body (leaves the
// plugin enabled, stops the supervised service).
func TestPluginServiceStop_ProxiesEnableManagedFalse(t *testing.T) {
	rec := &recordingClient{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.methods = append(rec.methods, r.Method)
		rec.paths = append(rec.paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		rec.bodies = append(rec.bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"disabled","managed":false,"previous_managed":true}`))
	}))
	defer srv.Close()
	s := newPluginServiceWriteServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc"}
	res, err := s.handlePluginServiceStop(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginServiceStop: %v", err)
	}
	if len(rec.paths) != 1 || rec.paths[0] != "/api/plugins/svc/enable" {
		t.Errorf("paths = %v, want [/api/plugins/svc/enable]", rec.paths)
	}
	if rec.bodies[0] != `{"managed":false}` {
		t.Errorf("body = %q, want {\"managed\":false}", rec.bodies[0])
	}
	if res == nil || len(res.Content) == 0 || !strings.Contains(res.Content[0].(mcpsdk.TextContent).Text, `"managed":false`) {
		t.Errorf("content does not surface the REST response: %v", res)
	}
}

// TestPluginServiceRestart_StopsThenStarts pins the "restart"
// contract: two calls to the same route, managed:false first, then
// managed:true.
func TestPluginServiceRestart_StopsThenStarts(t *testing.T) {
	rec := &recordingClient{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.methods = append(rec.methods, r.Method)
		rec.paths = append(rec.paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		rec.bodies = append(rec.bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"enabled"}`))
	}))
	defer srv.Close()
	s := newPluginServiceWriteServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc"}
	res, err := s.handlePluginServiceRestart(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginServiceRestart: %v", err)
	}
	want := []string{"POST /api/plugins/svc/enable {\"managed\":false}", "POST /api/plugins/svc/enable {\"managed\":true}"}
	if len(rec.paths) != 2 {
		t.Fatalf("expected exactly two REST calls (stop then start), got %v", rec.paths)
	}
	for i, line := range want {
		got := rec.methods[i] + " " + rec.paths[i] + " " + rec.bodies[i]
		if got != line {
			t.Errorf("call %d = %q, want %q", i+1, got, line)
		}
	}
	out := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(out, `"stop"`) || !strings.Contains(out, `"start"`) {
		t.Errorf("restart result should surface both legs, got %s", out)
	}
}

// TestPluginServiceWriteAttachOnlyRefusalIsClear: when the server
// refuses because the plugin does not declare managed lifecycle
// support (the REST 400 every attach-only plugin gets), all three
// lifecycle tools surface a clear, operator-facing refusal — and the
// restart tool must not attempt the start leg after the stop leg is
// refused.
func TestPluginServiceWriteAttachOnlyRefusalIsClear(t *testing.T) {
	rec := &recordingClient{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.paths = append(rec.paths, r.URL.Path)
		http.Error(w, `plugin "attach" does not declare managed lifecycle support`, http.StatusBadRequest)
	}))
	defer srv.Close()
	s := newPluginServiceWriteServer(t, srv)

	for _, tc := range []struct {
		name string
		args map[string]any
		call func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	}{
		{"plugin_service_start", map[string]any{"name": "attach"}, s.handlePluginServiceStart},
		{"plugin_service_stop", map[string]any{"name": "attach"}, s.handlePluginServiceStop},
		{"plugin_service_restart", map[string]any{"name": "attach"}, s.handlePluginServiceRestart},
	} {
		rec.paths = nil
		req := mcpsdk.CallToolRequest{}
		req.Params.Arguments = tc.args
		res, err := tc.call(context.Background(), req)
		if err == nil {
			t.Errorf("%s: expected the attach-only refusal as an error, got result %+v", tc.name, res)
			continue
		}
		if res != nil {
			t.Errorf("%s: expected no result alongside the refusal, got %+v", tc.name, res)
		}
		msg := err.Error()
		if !strings.Contains(msg, "attach-only") {
			t.Errorf("%s: refusal message must say the plugin is attach-only, got %q", tc.name, msg)
		}
		if !strings.Contains(msg, "managed") {
			t.Errorf("%s: refusal must explain the managed lifecycle mode, got %q", tc.name, msg)
		}
	}
	// The restart tool stops at the refused leg; it never starts.
	if len(rec.paths) != 1 {
		t.Errorf("restart on an attach-only plugin made %d REST calls, want exactly 1 (the refused stop)", len(rec.paths))
	}
}

// Argument validation: missing required arguments are refused before
// any HTTP call is made.
func TestPluginServiceWriteTools_RefuseEmptyArguments(t *testing.T) {
	s := &Server{} // webPort 0 — any proxy attempt would fail loudly
	cases := []struct {
		name string
		args map[string]any
		call func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	}{
		{"plugin_service_start", map[string]any{}, s.handlePluginServiceStart},
		{"plugin_service_start", map[string]any{"name": ""}, s.handlePluginServiceStart},
		{"plugin_service_stop", map[string]any{}, s.handlePluginServiceStop},
		{"plugin_service_stop", map[string]any{"name": ""}, s.handlePluginServiceStop},
		{"plugin_service_restart", map[string]any{}, s.handlePluginServiceRestart},
		{"plugin_token_rotate", map[string]any{}, s.handlePluginTokenRotate},
		{"plugin_token_rotate", map[string]any{"name": "svc"}, s.handlePluginTokenRotate},
		{"plugin_token_rotate", map[string]any{"name": "svc", "secret": "svc_key"}, s.handlePluginTokenRotate},
		{"plugin_token_rotate", map[string]any{"name": "svc", "secret": "svc_key", "new_value": ""}, s.handlePluginTokenRotate},
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

// TestPluginTokenRotate_NeverEchoesValue: the new value goes out to
// the server, and the tool response never contains it — even if the
// upstream response itself somehow carried it, because the tool
// re-emits a fixed field list instead of passing the body through.
func TestPluginTokenRotate_NeverEchoesValue(t *testing.T) {
	secret := "svc_api_key"
	newValue := "s3cr3t-new-value-XYZ"
	rec := &recordingClient{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.methods = append(rec.methods, r.Method)
		rec.paths = append(rec.paths, r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		rec.bodies = append(rec.bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		// A deliberately over-talking upstream: it echoes the value.
		// The tool must still not repeat it to the caller.
		_, _ = w.Write([]byte(`{"status":"rotated","plugin":"svc","secret":"` + secret + `","value":"` + newValue + `","echo":{"value":"` + newValue + `"}}`))
	}))
	defer srv.Close()
	s := newPluginServiceWriteServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc", "secret": secret, "new_value": newValue}
	res, err := s.handlePluginTokenRotate(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePluginTokenRotate: %v", err)
	}
	// The value WAS sent to the server (that is the job)…
	if len(rec.bodies) != 1 || !strings.Contains(rec.bodies[0], newValue) {
		t.Errorf("request body = %v, want the new value sent to the server", rec.bodies)
	}
	if rec.paths[0] != "/api/plugins/svc/tokens/"+secret+"/rotate" {
		t.Errorf("path = %q, want /api/plugins/svc/tokens/%s/rotate", rec.paths[0], secret)
	}
	// …and the response does NOT contain it.
	out := res.Content[0].(mcpsdk.TextContent).Text
	if strings.Contains(out, newValue) {
		t.Errorf("tool response echoes the new value: %s", out)
	}
	for _, want := range []string{`"status":"rotated"`, `"plugin":"svc"`, `"secret":"` + secret + `"`} {
		if !strings.Contains(out, want) {
			t.Errorf("tool response missing %s, got %s", want, out)
		}
	}
}

// TestPluginTokenRotate_UpstreamRefusalSurfaces: an unknown secret
// (404 from the guard) surfaces as a tool error, not a success.
func TestPluginTokenRotate_UpstreamRefusalSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `plugin "svc" does not reference secret "ghost_key"`, http.StatusBadRequest)
	}))
	defer srv.Close()
	s := newPluginServiceWriteServer(t, srv)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "svc", "secret": "ghost_key", "new_value": "x"}
	res, err := s.handlePluginTokenRotate(context.Background(), req)
	if err == nil {
		t.Errorf("expected an error for the upstream refusal, got %+v", res)
	}
}

// A cap entry exists for every new tool, and it is the plugin:write
// gate.
func TestPluginServiceWriteToolsHaveCapEntries(t *testing.T) {
	for _, name := range strings.Split(pluginServiceWriteNames, ", ") {
		cap, ok := federation.MCPToolCap[name]
		if !ok {
			t.Errorf("tool %q has no federation.MCPToolCap entry — POST /api/mcp/call would reject it", name)
			continue
		}
		if cap != federation.CapPluginWrite {
			t.Errorf("%s cap = %q, want %q", name, cap, federation.CapPluginWrite)
		}
	}
}
