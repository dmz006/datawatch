// Secret scan (git.secret_scan) MCP surface — tests for the
// secret_scan_config_get / secret_scan_config_set tools.
//
// Fakes only: an httptest.Server stands in for the daemon REST API
// (loopback); no real daemon, no gh, no external network.
// Pattern follows bl220_mcp_tools_test.go (detection / dns_channel / proxy
// config tool tests) and bl317_fed_auth_test.go (federation context).

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// firstText extracts the first TextContent payload from a tool result.
func firstSecretScanTestText(res *mcpsdk.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if txt, ok := res.Content[0].(mcpsdk.TextContent); ok {
		return txt.Text
	}
	return ""
}

// ── tool definitions ──

func TestSecretScanConfig_ToolNamesAndParams(t *testing.T) {
	s := &Server{}
	if got := s.toolSecretScanConfigGet().Name; got != "secret_scan_config_get" {
		t.Errorf("get tool name = %q, want secret_scan_config_get", got)
	}
	set := s.toolSecretScanConfigSet()
	if set.Name != "secret_scan_config_set" {
		t.Errorf("set tool name = %q, want secret_scan_config_set", set.Name)
	}
	for _, p := range []string{"mode", "detectors", "allow_list_paths", "allow_list_fingerprints"} {
		prop, ok := set.InputSchema.Properties[p]
		if !ok {
			t.Errorf("set tool is missing parameter %q", p)
			continue
		}
		m, ok := prop.(map[string]any)
		if !ok {
			t.Errorf("set tool parameter %s should be a JSON schema object, got %T", p, prop)
			continue
		}
		if typ, _ := m["type"].(string); typ != "string" {
			t.Errorf("set tool parameter %s type = %v, want string", p, m["type"])
		}
	}
}

// ── handleSecretScanConfigGet ──

func TestSecretScanConfigGet_ReturnsGitSection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/config" {
			http.Error(w, "not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"git": map[string]any{
				"secret_scan": map[string]any{
					"mode":      "shadow",
					"detectors": []string{"github_token", "private_key"},
					"allow_list": map[string]any{
						"paths":        []string{"testdata"},
						"fingerprints": []string{"0123456789abcdef"},
					},
				},
			},
			"other": "noise",
		})
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	res, err := s.handleSecretScanConfigGet(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected non-error result, got %+v", res)
	}
	text := firstSecretScanTestText(res)
	for _, want := range []string{"shadow", "github_token", "testdata", "0123456789abcdef"} {
		if !strings.Contains(text, want) {
			t.Errorf("result should contain %q, got: %s", want, text)
		}
	}
	if strings.Contains(text, "noise") {
		t.Errorf("result should not include non-git sections, got: %s", text)
	}
}

func TestSecretScanConfigGet_BackendError_ToolError(t *testing.T) {
	// HTTP 500 from the fake backend.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	res, err := s.handleSecretScanConfigGet(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("expected tool-error path, got Go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError result for HTTP 500, got %+v", res)
	}
	if text := firstSecretScanTestText(res); !strings.Contains(text, "secret_scan_config_get") || !strings.Contains(text, "HTTP 500") {
		t.Errorf("tool error should name the op and status, got %q", text)
	}

	// Unreachable backend: bind a listener, close it, then hit that port.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	port2, _ := strconv.Atoi(strings.Split(srv2.URL, ":")[2])
	srv2.Close()
	s2 := &Server{webPort: port2}

	res2, err2 := s2.handleSecretScanConfigGet(context.Background(), mcpsdk.CallToolRequest{})
	if err2 != nil {
		t.Fatalf("expected tool-error path for unreachable backend, got Go error: %v", err2)
	}
	if res2 == nil || !res2.IsError {
		t.Fatalf("expected IsError result for unreachable backend, got %+v", res2)
	}
}

// ── handleSecretScanConfigSet ──

func TestSecretScanConfigSet_DisabledByAllowSelfConfig_NoBackendCall(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: config.BoolPtr(false)},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"mode": "enforce"}
	res, err := s.handleSecretScanConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	// allowSelfConfigCheck carries the denial as flagged text (no IsError,
	// matching the generic config_set / BL220 config tools).
	text := firstSecretScanTestText(res)
	if !strings.Contains(text, "allow_self_config") || !strings.Contains(text, "permission denied") {
		t.Errorf("expected allow_self_config denial from allowSelfConfigCheck, got %q", text)
	}
	if calls != 0 {
		t.Errorf("fake backend saw %d calls, want 0", calls)
	}
}

func TestSecretScanConfigSet_AllFourParams_PutsCorrectPatch(t *testing.T) {
	var captured map[string]any
	puts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" {
			http.Error(w, "not found", 404)
			return
		}
		puts++
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"mode":                    "enforce",
		"detectors":               "github_token,private_key",
		"allow_list_paths":        "testdata,vendor",
		"allow_list_fingerprints": "0123456789abcdef",
	}

	res, err := s.handleSecretScanConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got tool error: %q", firstSecretScanTestText(res))
	}
	if puts != 1 {
		t.Fatalf("expected exactly one PUT /api/config, saw %d", puts)
	}
	want := map[string]any{
		"git.secret_scan.mode":                    "enforce",
		"git.secret_scan.detectors":               "github_token,private_key",
		"git.secret_scan.allow_list.paths":        "testdata,vendor",
		"git.secret_scan.allow_list.fingerprints": "0123456789abcdef",
	}
	for k, v := range want {
		if got, ok := captured[k]; !ok || got != v {
			t.Errorf("patch[%s] = %v (present=%v), want %v; full body: %v", k, got, ok, v, captured)
		}
	}
	if len(captured) != len(want) {
		t.Errorf("patch has %d keys, want exactly %d: %v", len(captured), len(want), captured)
	}
}

func TestSecretScanConfigSet_OnlyMode_PatchesOnlyMode(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)                    //nolint:errcheck
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"mode": "shadow"}

	res, err := s.handleSecretScanConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got tool error: %q", firstSecretScanTestText(res))
	}
	if got := captured["git.secret_scan.mode"]; got != "shadow" {
		t.Errorf("git.secret_scan.mode = %v, want shadow; full body: %v", got, captured)
	}
	for _, k := range []string{
		"git.secret_scan.detectors",
		"git.secret_scan.allow_list.paths",
		"git.secret_scan.allow_list.fingerprints",
	} {
		if _, ok := captured[k]; ok {
			t.Errorf("%s must not be blanked when unset: %v", k, captured)
		}
	}
	if len(captured) != 1 {
		t.Errorf("patch should contain only git.secret_scan.mode, got %v", captured)
	}
}

func TestSecretScanConfigSet_NoFields_NoOp_NoRequest(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleSecretScanConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got tool error: %q", firstSecretScanTestText(res))
	}
	if text := firstSecretScanTestText(res); !strings.Contains(text, "nothing updated") {
		t.Errorf("expected 'nothing updated' message, got %q", text)
	}
	if calls != 0 {
		t.Errorf("fake backend saw %d calls, want 0", calls)
	}
}

func TestSecretScanConfigSet_BackendError_ToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"mode": "enforce"}
	res, err := s.handleSecretScanConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("expected tool-error path, got Go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError result for HTTP 500, got %+v", res)
	}
	if text := firstSecretScanTestText(res); !strings.Contains(text, "secret_scan_config_set") || !strings.Contains(text, "HTTP 500") {
		t.Errorf("tool error should name the op and status, got %q", text)
	}
}

// ── federation capability enforcement ──

func TestSecretScanConfigSet_FedCap_Denied(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
	peer := &multiserver.Entry{
		Name:         "reader-peer",
		Federated:    true,
		Capabilities: []string{federation.CapConfigRead}, // no config:write
	}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"mode": "enforce"}
	res, err := s.handleSecretScanConfigSet(ctx, req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected fed-cap denial, got %+v", res)
	}
	if text := firstSecretScanTestText(res); !strings.Contains(text, "capability") {
		t.Errorf("denial should mention the missing capability, got %q", text)
	}
	if calls != 0 {
		t.Errorf("fake backend saw %d calls, want 0 (denied before any request)", calls)
	}
}

func TestSecretScanConfigSet_FedCap_Allowed_Proceeds(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)                    //nolint:errcheck
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
	peer := &multiserver.Entry{
		Name:         "writer-peer",
		Federated:    true,
		Capabilities: []string{federation.CapConfigWrite},
	}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"mode": "enforce"}
	res, err := s.handleSecretScanConfigSet(ctx, req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success for peer with config:write, got %q", firstSecretScanTestText(res))
	}
	if got := captured["git.secret_scan.mode"]; got != "enforce" {
		t.Errorf("git.secret_scan.mode = %v, want enforce; full body: %v", got, captured)
	}
}
