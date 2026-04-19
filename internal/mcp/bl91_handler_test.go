// BL91 — exercise MCP tool handlers directly (no stdio/SSE transport).
// Each test constructs a Server with a FakeTmux-backed session.Manager
// and invokes the handler, asserting on the returned text content.

package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/session"
)

func bl91Server(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	mgr.WithFakeTmux()
	return &Server{
		hostname: "testhost",
		manager:  mgr,
		dataDir:  dir,
		version:  "3.1.0-test",
	}
}

// call builds a CallToolRequest with the given args and invokes the handler.
func call(args map[string]any) mcpsdk.CallToolRequest {
	return mcpsdk.CallToolRequest{
		Params: mcpsdk.CallToolParams{Arguments: args},
	}
}

// resultText extracts the first TextContent from a CallToolResult.
func resultText(t *testing.T, res *mcpsdk.CallToolResult, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("empty result")
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("first content not text: %T", res.Content[0])
	}
	return tc.Text
}

type handlerFn func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)

func invoke(t *testing.T, h handlerFn, args map[string]any) string {
	t.Helper()
	res, err := h(context.Background(), call(args))
	return resultText(t, res, err)
}

func TestBL91_ListSessions_Empty(t *testing.T) {
	s := bl91Server(t)
	text := invoke(t, s.handleListSessions, nil)
	if !strings.Contains(text, "No active sessions") {
		t.Errorf("unexpected: %q", text)
	}
}

func TestBL91_ListSessions_IncludesSeeded(t *testing.T) {
	s := bl91Server(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa", FullID: "testhost-aa", Hostname: "testhost",
		Task: "hello world", State: session.StateRunning,
		UpdatedAt: time.Now(),
	})
	text := invoke(t, s.handleListSessions, nil)
	if !strings.Contains(text, "aa") || !strings.Contains(text, "hello world") {
		t.Errorf("list missing session: %q", text)
	}
}

func TestBL91_GetVersion(t *testing.T) {
	s := bl91Server(t)
	text := invoke(t, s.handleGetVersion, nil)
	if !strings.Contains(text, "3.1.0-test") {
		t.Errorf("version not reported: %q", text)
	}
}

func TestBL91_SendInput_SessionNotFound(t *testing.T) {
	s := bl91Server(t)
	text := invoke(t, s.handleSendInput,
		map[string]any{"id": "does-not-exist", "input": "hi"})
	if !strings.Contains(strings.ToLower(text), "error") &&
		!strings.Contains(strings.ToLower(text), "not found") {
		t.Errorf("expected not-found error, got: %q", text)
	}
}

func TestBL91_SendInput_MissingID(t *testing.T) {
	s := bl91Server(t)
	text := invoke(t, s.handleSendInput,
		map[string]any{"input": "hi"})
	if !strings.Contains(strings.ToLower(text), "error") {
		t.Errorf("expected error for missing id: %q", text)
	}
}

func TestBL91_RenameSession_MissingArgs(t *testing.T) {
	s := bl91Server(t)
	text := invoke(t, s.handleRenameSession, map[string]any{})
	if !strings.Contains(strings.ToLower(text), "error") {
		t.Errorf("expected error: %q", text)
	}
}
