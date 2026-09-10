package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFrameScanner verifies Content-Length framing is parsed correctly.
func TestFrameScanner(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"ping","id":1}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	sc := newFrameScanner(strings.NewReader(frame))
	got, err := sc.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != body {
		t.Errorf("got %q, want %q", got, body)
	}
}

// TestSendMsg verifies the framed output format.
func TestSendMsg(t *testing.T) {
	var buf bytes.Buffer
	msg := jsonrpc{JSONRPC: "2.0", ID: 1, Result: map[string]interface{}{"ok": true}}
	if err := sendMsg(&buf, msg); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.HasPrefix(s, "Content-Length:") {
		t.Errorf("missing Content-Length header: %q", s)
	}
	if !strings.Contains(s, `"ok":true`) {
		t.Errorf("body not present: %q", s)
	}
}

// TestHandlePing verifies ping → empty result.
func TestHandlePing(t *testing.T) {
	var buf bytes.Buffer
	raw, _ := json.Marshal(jsonrpc{JSONRPC: "2.0", Method: "ping", ID: 42})
	if err := handle(&buf, Config{Engine: "bing", NumResults: 10}, raw); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	idx := strings.Index(s, "\r\n\r\n")
	if idx < 0 {
		t.Fatal("no header/body separator")
	}
	var resp jsonrpc
	if err := json.Unmarshal([]byte(s[idx+4:]), &resp); err != nil {
		t.Fatal(err)
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
	s := buf.String()
	if !strings.Contains(s, "web_search") {
		t.Errorf("expected web_search in tools list: %q", s)
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

// TestSearxngSearch verifies parsing against a mock SearXNG HTTP server.
func TestSearxngSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"Test","url":"https://example.com","content":"A test result."}]}`)
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
