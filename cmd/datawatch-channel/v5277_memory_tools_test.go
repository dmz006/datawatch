// v5.27.7 (BL212, datawatch#29) — tests for the new memory tools
// on the channel.js bridge (Go implementation in main.go).

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// fakeBridge spins up an httptest.Server that records every parent
// API call the bridge makes, so tests can verify the bridge plumbs
// MCP tool invocations through to the right /api/memory/* endpoint.
type fakeBridge struct {
	*bridge
	srv         *httptest.Server
	gotMethod   string
	gotPath     string
	gotBody     string
	respBody    string
	respStatus  int
}

func newFakeBridge(t *testing.T) *fakeBridge {
	fb := &fakeBridge{respStatus: 200, respBody: `{"ok":true}`}
	fb.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fb.gotMethod = r.Method
		fb.gotPath = r.URL.RequestURI()
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		fb.gotBody = string(buf[:n])
		w.WriteHeader(fb.respStatus)
		_, _ = w.Write([]byte(fb.respBody))
	}))
	t.Cleanup(fb.srv.Close)
	fb.bridge = &bridge{cfg: config{apiURL: fb.srv.URL}}
	return fb
}

func TestBridge_MemoryRemember_PostsToSave(t *testing.T) {
	fb := newFakeBridge(t)
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "memory_remember"
	req.Params.Arguments = map[string]any{"text": "remember me"}

	res, err := fb.handleMemoryRemember(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Fatalf("got tool error result")
	}
	if fb.gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", fb.gotMethod)
	}
	if fb.gotPath != "/api/memory/save" {
		t.Errorf("path=%q want /api/memory/save", fb.gotPath)
	}
	if !strings.Contains(fb.gotBody, "remember me") {
		t.Errorf("body should contain text; got %q", fb.gotBody)
	}
}

func TestBridge_MemoryRecall_GetsFromSearch(t *testing.T) {
	fb := newFakeBridge(t)
	fb.respBody = `[{"id":1,"content":"matched"}]`
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "memory_recall"
	req.Params.Arguments = map[string]any{"query": "find me"}

	res, err := fb.handleMemoryRecall(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Fatalf("got tool error result")
	}
	if fb.gotMethod != http.MethodGet {
		t.Errorf("method=%q want GET", fb.gotMethod)
	}
	if !strings.Contains(fb.gotPath, "/api/memory/search?q=") {
		t.Errorf("path=%q must hit /api/memory/search?q=", fb.gotPath)
	}
	if !strings.Contains(fb.gotPath, "find") {
		t.Errorf("path=%q should contain encoded query", fb.gotPath)
	}
}

func TestBridge_MemoryList_GetsFromList(t *testing.T) {
	fb := newFakeBridge(t)
	fb.respBody = `[]`
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "memory_list"
	req.Params.Arguments = map[string]any{"n": float64(50)}

	_, err := fb.handleMemoryList(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(fb.gotPath, "/api/memory/list?n=50") {
		t.Errorf("path=%q must include n=50", fb.gotPath)
	}
}

func TestBridge_MemoryForget_PostsDelete(t *testing.T) {
	fb := newFakeBridge(t)
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "memory_forget"
	req.Params.Arguments = map[string]any{"id": float64(42)}

	_, err := fb.handleMemoryForget(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if fb.gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", fb.gotMethod)
	}
	if fb.gotPath != "/api/memory/delete" {
		t.Errorf("path=%q want /api/memory/delete", fb.gotPath)
	}
	if !strings.Contains(fb.gotBody, "42") {
		t.Errorf("body should contain id=42; got %q", fb.gotBody)
	}
}

func TestBridge_MemoryStats_GetsFromStats(t *testing.T) {
	fb := newFakeBridge(t)
	fb.respBody = `{"total_count":36}`
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "memory_stats"

	res, err := fb.handleMemoryStats(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Fatalf("got tool error result")
	}
	if fb.gotMethod != http.MethodGet {
		t.Errorf("method=%q want GET", fb.gotMethod)
	}
	if fb.gotPath != "/api/memory/stats" {
		t.Errorf("path=%q want /api/memory/stats", fb.gotPath)
	}
}

func TestBridge_MemoryRemember_RejectsEmptyText(t *testing.T) {
	fb := newFakeBridge(t)
	req := mcpsdk.CallToolRequest{}
	req.Params.Name = "memory_remember"
	req.Params.Arguments = map[string]any{"text": ""}

	res, err := fb.handleMemoryRemember(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Errorf("empty text should produce a tool error result")
	}
	// Should have NOT made any HTTP call to the parent.
	if fb.gotMethod != "" {
		t.Errorf("bridge made an unexpected HTTP call: %s %s", fb.gotMethod, fb.gotPath)
	}
}

func TestUrlQueryEscape_HandlesSpacesAndSpecials(t *testing.T) {
	cases := map[string]string{
		"hello world":       "hello+world",
		"a/b":               "a%2Fb",
		"a&b=c":             "a%26b%3Dc",
		"alphaNum123-_.~":   "alphaNum123-_.~",
	}
	for in, want := range cases {
		if got := urlQueryEscape(in); got != want {
			t.Errorf("urlQueryEscape(%q) = %q want %q", in, got, want)
		}
	}
}
