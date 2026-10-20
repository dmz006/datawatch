package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// newRejudgeTestServer starts a fake REST endpoint that captures the last
// method, path and body it saw, and returns a *Server pointed at it.
func newRejudgeTestServer(t *testing.T, sinkPath *string, sinkMethod *string, sinkBody *map[string]any) *Server {
	t.Helper()
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, sinkBody)
		*sinkPath = r.URL.Path
		*sinkMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(rest.Close)

	_, portStr, err := net.SplitHostPort(rest.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}
}

// TestAutonomousPRDRejudgeForwardsActorOperator checks the handler POSTs
// {"actor":"operator"} to the rejudge endpoint, mirroring the existing
// proxy-handler tests (see clean_restart_test.go).
func TestAutonomousPRDRejudgeForwardsActorOperator(t *testing.T) {
	var gotPath, gotMethod string
	var got map[string]any
	s := newRejudgeTestServer(t, &gotPath, &gotMethod, &got)

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": "prd-rej-1"}

	res, err := s.handleAutonomousPRDRejudge(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res == nil {
		t.Fatal("expected a tool result, got nil")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("REST method = %q; want POST", gotMethod)
	}
	if want := "/api/autonomous/prds/prd-rej-1/rejudge"; gotPath != want {
		t.Errorf("REST path = %q; want %q", gotPath, want)
	}
	if got["actor"] != "operator" {
		t.Errorf("REST body actor = %v; want \"operator\" (body=%v)", got["actor"], got)
	}
}

// TestAutonomousPRDRejudgeToolDefinition checks the MCP tool definition:
// name, the single required id string param, no task_id, and a description
// that says it re-judges the blocked PRD's completion guardrail without
// resetting any task and refuses otherwise.
func TestAutonomousPRDRejudgeToolDefinition(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousPRDRejudge()
	if tool.Name != "autonomous_prd_rejudge" {
		t.Errorf("tool name = %q; want autonomous_prd_rejudge", tool.Name)
	}
	if len(tool.InputSchema.Properties) == 0 {
		t.Fatal("tool has no input schema properties")
	}
	props := tool.InputSchema.Properties
	if _, ok := props["id"]; !ok {
		t.Error("input schema is missing the \"id\" property")
	}
	if _, ok := props["task_id"]; ok {
		t.Error("input schema must not take a task_id: rejudge re-evaluates the guardrail without resetting any task")
	}
	desc := strings.ToLower(tool.Description)
	for _, word := range []string{"blocked", "guardrail", "refus"} {
		if !strings.Contains(desc, word) {
			t.Errorf("tool description missing %q keyword: %q", word, tool.Description)
		}
	}
}
