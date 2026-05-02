// BL220 — MCP tool tests for detection / dns_channel / proxy surface parity.

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
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── tool name assertions ──────────────────────────────────────────────────────

func TestBL220_ToolNames(t *testing.T) {
	s := &Server{}
	cases := []struct {
		want string
		got  string
	}{
		{"detection_status", s.toolDetectionStatus().Name},
		{"detection_config_get", s.toolDetectionConfigGet().Name},
		{"detection_config_set", s.toolDetectionConfigSet().Name},
		{"dns_channel_config_get", s.toolDNSChannelConfigGet().Name},
		{"dns_channel_config_set", s.toolDNSChannelConfigSet().Name},
		{"proxy_config_get", s.toolProxyConfigGet().Name},
		{"proxy_config_set", s.toolProxyConfigSet().Name},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("tool name = %q, want %q", c.got, c.want)
		}
	}
}

// ── configSection helper ──────────────────────────────────────────────────────

func TestBL220_ConfigSection_ExtractsSection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			http.Error(w, "not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"detection": map[string]any{
				"prompt_debounce":  3,
				"notify_cooldown": 15,
			},
			"other": "noise",
		})
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out, err := s.configSection("detection")
	if err != nil {
		t.Fatalf("configSection: %v", err)
	}
	if !strings.Contains(out, "prompt_debounce") {
		t.Errorf("expected detection section, got: %s", out)
	}
	if strings.Contains(out, "other") {
		t.Errorf("should not include other keys, got: %s", out)
	}
}

func TestBL220_ConfigSection_MissingSection_ReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"something": 1}) //nolint:errcheck
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	out, err := s.configSection("missing_key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "{}" {
		t.Errorf("expected {}, got %q", out)
	}
}

// ── loopback-unconfigured safety ──────────────────────────────────────────────

func TestBL220_LoopbackUnconfigured(t *testing.T) {
	s := &Server{}
	ctx := context.Background()
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	tools := []struct {
		name    string
		handler func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)
	}{
		{"detection_status", s.handleDetectionStatus},
		{"detection_config_get", s.handleDetectionConfigGet},
		{"dns_channel_config_get", s.handleDNSChannelConfigGet},
		{"proxy_config_get", s.handleProxyConfigGet},
	}
	for _, tc := range tools {
		res, err := tc.handler(ctx, req)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
		if res == nil {
			t.Errorf("%s: nil result", tc.name)
			continue
		}
		text := ""
		if len(res.Content) > 0 {
			if txt, ok := res.Content[0].(mcpsdk.TextContent); ok {
				text = txt.Text
			}
		}
		if !strings.Contains(text, "Error") && !strings.Contains(text, "unavailable") {
			t.Errorf("%s: expected error text, got %q", tc.name, text)
		}
	}
}

// ── allowSelfConfigCheck gate ─────────────────────────────────────────────────

func TestBL220_AllowSelfConfigCheck_Denied(t *testing.T) {
	s := &Server{cfg: &config.MCPConfig{AllowSelfConfig: false}}
	res := s.allowSelfConfigCheck()
	if res == nil {
		t.Fatal("expected denied result, got nil")
	}
}

func TestBL220_AllowSelfConfigCheck_Allowed(t *testing.T) {
	s := &Server{cfg: &config.MCPConfig{AllowSelfConfig: true}}
	res := s.allowSelfConfigCheck()
	if res != nil {
		t.Fatalf("expected nil (allowed), got %+v", res)
	}
}

func TestBL220_AllowSelfConfigCheck_NilCfg(t *testing.T) {
	s := &Server{cfg: nil}
	res := s.allowSelfConfigCheck()
	if res == nil {
		t.Fatal("expected denied result with nil cfg")
	}
}

// ── detection_config_set propagates to PUT /api/config ───────────────────────

func TestBL220_DetectionConfigSet_PatchesCorrectKeys(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api/config" {
			json.NewDecoder(r.Body).Decode(&captured) //nolint:errcheck
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: true},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"prompt_debounce":  float64(5),
		"notify_cooldown": float64(30),
	}

	res, err := s.handleDetectionConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if v, ok := captured["detection.prompt_debounce"]; !ok || v != float64(5) {
		t.Errorf("detection.prompt_debounce not patched: %v", captured)
	}
	if v, ok := captured["detection.notify_cooldown"]; !ok || v != float64(30) {
		t.Errorf("detection.notify_cooldown not patched: %v", captured)
	}
}

// ── proxy_config_set propagates to PUT /api/config ───────────────────────────

func TestBL220_ProxyConfigSet_PatchesCorrectKeys(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api/config" {
			json.NewDecoder(r.Body).Decode(&captured) //nolint:errcheck
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: true},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"health_interval": float64(60),
		"request_timeout": float64(20),
	}

	_, err := s.handleProxyConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := captured["proxy.health_interval"]; !ok || v != float64(60) {
		t.Errorf("proxy.health_interval not patched: %v", captured)
	}
	if v, ok := captured["proxy.request_timeout"]; !ok || v != float64(20) {
		t.Errorf("proxy.request_timeout not patched: %v", captured)
	}
}

// ── no-op when no fields provided ────────────────────────────────────────────

func TestBL220_DetectionConfigSet_NoFields_NoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{
		webPort: port,
		cfg:     &config.MCPConfig{AllowSelfConfig: true},
	}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleDetectionConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("PUT /api/config should not be called when no fields provided")
	}
	text := ""
	if len(res.Content) > 0 {
		if txt, ok := res.Content[0].(mcpsdk.TextContent); ok {
			text = txt.Text
		}
	}
	if !strings.Contains(text, "nothing updated") {
		t.Errorf("expected 'nothing updated' message, got %q", text)
	}
}
