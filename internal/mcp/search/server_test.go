package search

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/websearch"
)

// emptyRegistry is a registry with no providers — exercises the
// "not configured" branch without needing a live backend.
func emptyRegistry(t *testing.T) *websearch.Registry {
	t.Helper()
	r, err := websearch.NewRegistry(nil, nil, nil, 0)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

// workingRegistry is a registry with one SearXNG provider pointed at an
// httptest server that always returns one fixed result.
func workingRegistry(t *testing.T) *websearch.Registry {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Test","url":"https://example.com","content":"A test result."}]}`))
	}))
	t.Cleanup(srv.Close)
	r, err := websearch.NewRegistry([]websearch.ProviderSpec{
		{Name: "test", Type: "searxng", Enabled: true, URL: srv.URL, Engine: "bing", NumResults: 10},
	}, nil, nil, 0)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

// TestSendMsg verifies NDJSON output: one JSON object per line, no Content-Length header.
func TestSendMsg(t *testing.T) {
	var buf bytes.Buffer
	msg := jsonrpc{JSONRPC: "2.0", ID: 1, Result: map[string]interface{}{"ok": true}}
	if err := sendMsg(&buf, msg); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if strings.HasPrefix(s, "Content-Length:") {
		t.Errorf("NDJSON transport must not include Content-Length header: %q", s)
	}
	if !strings.Contains(s, `"ok":true`) {
		t.Errorf("body not present: %q", s)
	}
	if !strings.HasSuffix(s, "\n") {
		t.Errorf("NDJSON message must end with newline: %q", s)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out); err != nil {
		t.Errorf("NDJSON line is not valid JSON: %v", err)
	}
}

// TestHandlePing verifies ping → empty result via NDJSON.
func TestHandlePing(t *testing.T) {
	var buf bytes.Buffer
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "ping", ID: 42})
	if err := handle(&buf, emptyRegistry(t), 10, "", raw); err != nil {
		t.Fatal(err)
	}
	var resp jsonrpc
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v (got %q)", err, buf.String())
	}
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}
}

// TestHandleToolsList verifies the tools list response.
func TestHandleToolsList(t *testing.T) {
	var buf bytes.Buffer
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "tools/list", ID: 1})
	if err := handle(&buf, emptyRegistry(t), 5, "", raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "web_search") {
		t.Errorf("expected web_search in tools list: %q", buf.String())
	}
}

// TestHandleToolsCallNoProviders verifies an error is returned when no
// providers are configured/enabled.
func TestHandleToolsCallNoProviders(t *testing.T) {
	var buf bytes.Buffer
	params, _ := json.Marshal(map[string]interface{}{
		"name":      "web_search",
		"arguments": map[string]interface{}{"query": "test"},
	})
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "tools/call", ID: 1, Params: params})
	if err := handle(&buf, emptyRegistry(t), 10, "", raw); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "no enabled web_search providers") {
		t.Errorf("expected error text about no providers, got: %q", s)
	}
}

// TestHandleToolsCallWithRegistry verifies a real search round-trips
// through handle() into the registry and back out as MCP tool content.
func TestHandleToolsCallWithRegistry(t *testing.T) {
	var buf bytes.Buffer
	params, _ := json.Marshal(map[string]interface{}{
		"name":      "web_search",
		"arguments": map[string]interface{}{"query": "test query"},
	})
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "tools/call", ID: 1, Params: params})
	if err := handle(&buf, workingRegistry(t), 10, "sess-1", raw); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "Test") || !strings.Contains(s, "example.com") {
		t.Errorf("expected result content in response, got: %q", s)
	}
	if strings.Contains(s, `"isError":true`) {
		t.Errorf("expected isError:false for a successful search, got: %q", s)
	}
}

// TestHandleToolsCallUnknownTool verifies a request for a tool other than
// web_search is rejected.
func TestHandleToolsCallUnknownTool(t *testing.T) {
	var buf bytes.Buffer
	params, _ := json.Marshal(map[string]interface{}{"name": "not_web_search", "arguments": map[string]interface{}{}})
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "tools/call", ID: 1, Params: params})
	if err := handle(&buf, emptyRegistry(t), 10, "", raw); err != nil {
		t.Fatal(err)
	}
	var resp jsonrpc
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.Error == nil {
		t.Error("expected error response for unknown tool name")
	}
}

// TestHandleUnknownMethod verifies unknown methods return a JSON-RPC error for requests with ID.
func TestHandleUnknownMethod(t *testing.T) {
	var buf bytes.Buffer
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "unknown/method", ID: 99})
	if err := handle(&buf, emptyRegistry(t), 10, "", raw); err != nil {
		t.Fatal(err)
	}
	var resp jsonrpc
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.Error == nil {
		t.Errorf("expected error response for unknown method")
	}
}

// TestHandleNotification verifies notifications (no ID) produce no response.
func TestHandleNotification(t *testing.T) {
	var buf bytes.Buffer
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "notifications/initialized"})
	if err := handle(&buf, emptyRegistry(t), 10, "", raw); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("notification must produce no response, got: %q", buf.String())
	}
}

// TestConfigFromEnv verifies env var parsing (standalone single-provider
// override path).
func TestConfigFromEnv(t *testing.T) {
	t.Setenv("DATAWATCH_WEB_SEARCH_URL", "http://searxng.example.com:3001")
	t.Setenv("DATAWATCH_WEB_SEARCH_ENGINE", "brave")
	t.Setenv("DATAWATCH_WEB_SEARCH_NUM_RESULTS", "5")

	cfg := ConfigFromEnv()
	if cfg.URL != "http://searxng.example.com:3001" {
		t.Errorf("URL mismatch: %q", cfg.URL)
	}
	if cfg.Engine != "brave" {
		t.Errorf("Engine mismatch: %q", cfg.Engine)
	}
	if cfg.NumResults != 5 {
		t.Errorf("NumResults mismatch: %d", cfg.NumResults)
	}
}

// TestConfigFromEnvDefaults verifies defaults when optional env vars are absent.
func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("DATAWATCH_WEB_SEARCH_URL", "http://searxng.example.com:3001")
	t.Setenv("DATAWATCH_WEB_SEARCH_ENGINE", "")
	t.Setenv("DATAWATCH_WEB_SEARCH_NUM_RESULTS", "")

	cfg := ConfigFromEnv()
	if cfg.NumResults != 10 {
		t.Errorf("default num_results should be 10, got: %d", cfg.NumResults)
	}
}

// TestBuildRegistryStandaloneOverride verifies the env-var single-provider
// override path builds a working one-provider SearXNG registry without
// needing a config.yaml at all.
func TestBuildRegistryStandaloneOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"T","url":"https://t.example","content":"c"}]}`))
	}))
	defer srv.Close()

	registry, err := buildRegistry(Config{URL: srv.URL, Engine: "bing", NumResults: 10})
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	if !registry.Enabled() {
		t.Fatal("expected a working registry from the override path")
	}
	names := registry.ProviderNames()
	if len(names) != 1 || names[0] != "override" {
		t.Errorf("ProviderNames() = %v, want [override]", names)
	}
}

// TestBuildRegistryFromConfigYAML verifies the real multi-provider path:
// loading config.yaml's web_search.providers[] list (replacing the old
// pre-BL391 single-field web_search.url/engine shape this test used to
// exercise via readDaemonConfig).
func TestBuildRegistryFromConfigYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATAWATCH_WEB_SEARCH_URL", "")
	t.Setenv("DATAWATCH_WEB_SEARCH_ENGINE", "")

	yaml := "web_search:\n" +
		"  enabled: true\n" +
		"  providers:\n" +
		"    - name: searxng-local\n" +
		"      type: searxng\n" +
		"      enabled: true\n" +
		"      url: http://searxng.local:3001\n" +
		"      engine: bing\n"
	if err := os.WriteFile(dir+"/config.yaml", []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	registry, err := buildRegistry(Config{DataDir: dir, NumResults: 10})
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	names := registry.ProviderNames()
	if len(names) != 1 || names[0] != "searxng-local" {
		t.Errorf("ProviderNames() = %v, want [searxng-local]", names)
	}
}
