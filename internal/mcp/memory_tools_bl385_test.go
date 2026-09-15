// BL385 Phase 3 — MCP subprocess memory scope routing tests.
//
// TC-1:  subprocessMode() false when callerSessionID empty
// TC-2:  subprocessMode() true when callerSessionID set
// TC-3:  SetCallerPRDID / SetCallerStoryID setters
// TC-4:  memory_remember in subprocess mode routes to scopes/save
// TC-5:  memory_remember NOT in subprocess mode uses direct path
// TC-6:  memory_recall in subprocess mode routes to scopes/recall
// TC-7:  memory_recall in subprocess mode includes prd_id param
// TC-8:  memory_recall in subprocess mode includes story_id param
// TC-9:  memory_recall NOT in subprocess mode uses direct search
// TC-10: memory_list in subprocess mode routes to scopes/recall with session
// TC-11: memory_sweep_stale blocked in subprocess mode
// TC-12: memory_import blocked in subprocess mode

package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// bl385Proxy records the last POST/GET path and body/query sent through it.
type bl385Proxy struct {
	lastMethod string
	lastPath   string
	lastBody   string
	lastQuery  url.Values
	response   string
}

// mockProxyServer creates an httptest.Server and returns the proxy recorder
// and the port to inject into the MCP server.
func mockProxyServer(t *testing.T, rec *bl385Proxy) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.lastMethod = r.Method
		rec.lastPath = r.URL.Path
		rec.lastQuery = r.URL.Query()
		bodyBytes, _ := io.ReadAll(r.Body)
		rec.lastBody = string(bodyBytes)
		resp := rec.response
		if resp == "" {
			resp = `{"ok":true}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newBL385Server(t *testing.T, rec *bl385Proxy) (*Server, *httptest.Server) {
	t.Helper()
	proxySrv := mockProxyServer(t, rec)
	// Parse port from httptest server URL.
	addr := proxySrv.Listener.Addr().String()
	portStr := addr[strings.LastIndex(addr, ":")+1:]
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	s := &Server{webPort: port, token: ""}
	return s, proxySrv
}

// TC-1: subprocessMode() false when callerSessionID empty.
func TestBL385_SubprocessMode_FalseWhenEmpty(t *testing.T) {
	s := &Server{}
	if s.subprocessMode() {
		t.Error("subprocessMode() should be false when callerSessionID is empty")
	}
}

// TC-2: subprocessMode() true when callerSessionID set.
func TestBL385_SubprocessMode_TrueWhenSet(t *testing.T) {
	s := &Server{callerSessionID: "sess-abc"}
	if !s.subprocessMode() {
		t.Error("subprocessMode() should be true when callerSessionID is set")
	}
}

// TC-3: SetCallerPRDID / SetCallerStoryID store values.
func TestBL385_SetCallerIDs(t *testing.T) {
	s := &Server{}
	s.SetCallerPRDID("prd-123")
	s.SetCallerStoryID("story-456")
	if s.callerPRDID != "prd-123" {
		t.Errorf("callerPRDID = %q, want prd-123", s.callerPRDID)
	}
	if s.callerStoryID != "story-456" {
		t.Errorf("callerStoryID = %q, want story-456", s.callerStoryID)
	}
}

// TC-4: memory_remember in subprocess mode routes to /api/memory/scopes/save.
func TestBL385_MemoryRemember_SubprocessMode_RoutesToScopedSave(t *testing.T) {
	rec := &bl385Proxy{}
	s, _ := newBL385Server(t, rec)
	s.SetCallerSessionID("sess-abc")

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"text": "learned something"}
	_, _ = s.handleMemoryRemember(context.Background(), req)

	if rec.lastPath != "/api/memory/scopes/save" {
		t.Errorf("expected path /api/memory/scopes/save, got %q", rec.lastPath)
	}
	if !strings.Contains(rec.lastBody, "session-local") {
		t.Errorf("expected session-local scope in body, got %q", rec.lastBody)
	}
	if !strings.Contains(rec.lastBody, "sess-abc") {
		t.Errorf("expected session_id in body, got %q", rec.lastBody)
	}
}

// TC-5: memory_remember NOT in subprocess mode uses /api/memory/save.
func TestBL385_MemoryRemember_NormalMode_UsesDirectPath(t *testing.T) {
	rec := &bl385Proxy{}
	s, _ := newBL385Server(t, rec)
	// No callerSessionID — normal mode.

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"text": "project learning"}
	_, _ = s.handleMemoryRemember(context.Background(), req)

	if rec.lastPath != "/api/memory/save" {
		t.Errorf("expected /api/memory/save, got %q", rec.lastPath)
	}
}

// TC-6: memory_recall in subprocess mode routes to /api/memory/scopes/recall.
func TestBL385_MemoryRecall_SubprocessMode_RoutesToScopedRecall(t *testing.T) {
	rec := &bl385Proxy{}
	s, _ := newBL385Server(t, rec)
	s.SetCallerSessionID("sess-abc")

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "auth flow"}
	_, _ = s.handleMemoryRecall(context.Background(), req)

	if rec.lastPath != "/api/memory/scopes/recall" {
		t.Errorf("expected /api/memory/scopes/recall, got %q", rec.lastPath)
	}
	if rec.lastQuery.Get("session") != "sess-abc" {
		t.Errorf("expected session=sess-abc in query, got %q", rec.lastQuery)
	}
}

// TC-7: memory_recall in subprocess mode includes prd_id when set.
func TestBL385_MemoryRecall_SubprocessMode_IncludesPRDID(t *testing.T) {
	rec := &bl385Proxy{}
	s, _ := newBL385Server(t, rec)
	s.SetCallerSessionID("sess-abc")
	s.SetCallerPRDID("prd-xyz")

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "retry logic"}
	_, _ = s.handleMemoryRecall(context.Background(), req)

	if rec.lastQuery.Get("prd_id") != "prd-xyz" {
		t.Errorf("expected prd_id=prd-xyz, got query=%v", rec.lastQuery)
	}
}

// TC-8: memory_recall in subprocess mode includes story_id when set.
func TestBL385_MemoryRecall_SubprocessMode_IncludesStoryID(t *testing.T) {
	rec := &bl385Proxy{}
	s, _ := newBL385Server(t, rec)
	s.SetCallerSessionID("sess-abc")
	s.SetCallerStoryID("story-999")

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "database schema"}
	_, _ = s.handleMemoryRecall(context.Background(), req)

	if rec.lastQuery.Get("story_id") != "story-999" {
		t.Errorf("expected story_id=story-999, got query=%v", rec.lastQuery)
	}
}

// TC-9: memory_recall NOT in subprocess mode uses /api/memory/search.
func TestBL385_MemoryRecall_NormalMode_UsesDirectSearch(t *testing.T) {
	rec := &bl385Proxy{}
	s, _ := newBL385Server(t, rec)
	// No callerSessionID.

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "some query"}
	_, _ = s.handleMemoryRecall(context.Background(), req)

	if rec.lastPath != "/api/memory/search" {
		t.Errorf("expected /api/memory/search, got %q", rec.lastPath)
	}
}

// TC-10: memory_list in subprocess mode routes to scopes/recall with session layer.
func TestBL385_MemoryList_SubprocessMode_RoutesToScopedRecall(t *testing.T) {
	rec := &bl385Proxy{}
	s, _ := newBL385Server(t, rec)
	s.SetCallerSessionID("sess-abc")

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	_, _ = s.handleMemoryList(context.Background(), req)

	if rec.lastPath != "/api/memory/scopes/recall" {
		t.Errorf("expected /api/memory/scopes/recall, got %q", rec.lastPath)
	}
	if rec.lastQuery.Get("session") != "sess-abc" {
		t.Errorf("expected session=sess-abc in query, got %v", rec.lastQuery)
	}
	if rec.lastQuery.Get("layers") != "session-local" {
		t.Errorf("expected layers=session-local in query, got %v", rec.lastQuery)
	}
}

// TC-11: memory_sweep_stale blocked in subprocess mode.
func TestBL385_MemorySweep_BlockedInSubprocessMode(t *testing.T) {
	s := &Server{callerSessionID: "sess-abc"}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	result, err := s.handleMemorySweep(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	content := ""
	for _, c := range result.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			content = tc.Text
		}
	}
	if !strings.Contains(content, "not available in subprocess mode") {
		t.Errorf("expected blocked message, got %q", content)
	}
}

// TC-12: memory_import blocked in subprocess mode.
func TestBL385_MemoryImport_BlockedInSubprocessMode(t *testing.T) {
	s := &Server{callerSessionID: "sess-abc"}
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"json_data": "[]"}
	result, err := s.handleMemoryImport(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	content := ""
	for _, c := range result.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			content = tc.Text
		}
	}
	if !strings.Contains(content, "not available in subprocess mode") {
		t.Errorf("expected blocked message, got %q", content)
	}
}
