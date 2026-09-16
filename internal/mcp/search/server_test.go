package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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
	if err := handle(&buf, Config{Engine: "bing", NumResults: 10}, raw); err != nil {
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
	if err := handle(&buf, Config{Engine: "bing", NumResults: 5}, raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "web_search") {
		t.Errorf("expected web_search in tools list: %q", buf.String())
	}
}

// TestHandleToolsCallNoURL verifies an error is returned when URL is not set.
func TestHandleToolsCallNoURL(t *testing.T) {
	var buf bytes.Buffer
	params, _ := json.Marshal(map[string]interface{}{
		"name":      "web_search",
		"arguments": map[string]interface{}{"query": "test"},
	})
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "tools/call", ID: 1, Params: params})
	if err := handle(&buf, Config{Engine: "bing", NumResults: 10}, raw); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "not configured") && !strings.Contains(s, "Search error") {
		t.Errorf("expected error text about missing URL, got: %q", s)
	}
}

// TestHandleUnknownMethod verifies unknown methods return a JSON-RPC error for requests with ID.
func TestHandleUnknownMethod(t *testing.T) {
	var buf bytes.Buffer
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "unknown/method", ID: 99})
	if err := handle(&buf, Config{Engine: "bing", NumResults: 10}, raw); err != nil {
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
	if err := handle(&buf, Config{Engine: "bing", NumResults: 10}, raw); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("notification must produce no response, got: %q", buf.String())
	}
}

// TestSearxngSearch verifies parsing against a mock SearXNG HTTP server.
func TestSearxngSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"results":[{"title":"Test","url":"https://example.com","content":"A test result."}]}`)
	}))
	defer srv.Close()

	cfg := Config{URL: srv.URL, Engine: "bing", NumResults: 10}
	results, err := searxngSearch(cfg, "test query", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Test" {
		t.Errorf("unexpected title: %q", results[0].Title)
	}
}

// TestSearxngSearchLimit verifies the limit parameter is respected.
func TestSearxngSearchLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"results":[
			{"title":"R1","url":"https://r1.com","content":"c1"},
			{"title":"R2","url":"https://r2.com","content":"c2"},
			{"title":"R3","url":"https://r3.com","content":"c3"}
		]}`)
	}))
	defer srv.Close()

	results, err := searxngSearch(Config{URL: srv.URL, Engine: "bing", NumResults: 10}, "q", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("limit=2: expected 2 results, got %d", len(results))
	}
}

// TestSearxngSearchSnippetTrunc verifies long content is truncated to 400 chars.
func TestSearxngSearchSnippetTrunc(t *testing.T) {
	long := strings.Repeat("x", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "T", "url": "https://t.com", "content": long},
			},
		})
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	results, err := searxngSearch(Config{URL: srv.URL, Engine: "bing", NumResults: 10}, "q", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].Content) != 400 {
		t.Errorf("expected snippet truncated to 400 chars, got %d", len(results[0].Content))
	}
}

// TestConfigFromEnv verifies env var parsing.
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
	if cfg.Engine != "bing" {
		t.Errorf("default engine should be bing, got: %q", cfg.Engine)
	}
	if cfg.NumResults != 10 {
		t.Errorf("default num_results should be 10, got: %d", cfg.NumResults)
	}
}

// TestReadDaemonConfig verifies config.yaml fallback parsing.
func TestReadDaemonConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATAWATCH_DATA_DIR", dir)
	t.Setenv("DATAWATCH_WEB_SEARCH_URL", "")
	t.Setenv("DATAWATCH_WEB_SEARCH_ENGINE", "")

	yaml := "other_key: value\nweb_search:\n  url: http://searxng.local:3001\n  engine: google\nanother_key: value\n"
	if err := os.WriteFile(dir+"/config.yaml", []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := ConfigFromEnv()
	if cfg.URL != "http://searxng.local:3001" {
		t.Errorf("URL from config.yaml: got %q, want http://searxng.local:3001", cfg.URL)
	}
	if cfg.Engine != "google" {
		t.Errorf("Engine from config.yaml: got %q, want google", cfg.Engine)
	}
}
