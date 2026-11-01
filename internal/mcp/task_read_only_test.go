package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// readOnlyLoopback stands in for the REST loopback and records the last
// request the MCP handler sent.
func readOnlyLoopback(t *testing.T) (*Server, *map[string]any, *string) {
	t.Helper()
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(raw, &body)
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"p1"}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return &Server{webPort: port}, &body, &path
}

func toolReq(args map[string]any) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

func TestMCPEditTaskForwardsReadOnlyOnlyWhenGiven(t *testing.T) {
	s, body, path := readOnlyLoopback(t)
	if _, err := s.handleAutonomousPRDEditTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "task_id": "t1", "read_only": true,
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.HasSuffix(*path, "/p1/edit_task") {
		t.Fatalf("path = %q", *path)
	}
	if v, ok := (*body)["read_only"].(bool); !ok || !v {
		t.Fatalf("body read_only = %v, want true (body %v)", (*body)["read_only"], *body)
	}
	// false is forwarded as an explicit clear.
	_, _ = s.handleAutonomousPRDEditTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "task_id": "t1", "read_only": false,
	}))
	if v, ok := (*body)["read_only"].(bool); !ok || v {
		t.Fatalf("body read_only = %v, want an explicit false", (*body)["read_only"])
	}
	// A spec-only edit must not send read_only, or it would clear the mark.
	_, _ = s.handleAutonomousPRDEditTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "task_id": "t1", "new_spec": "reworded",
	}))
	if _, present := (*body)["read_only"]; present {
		t.Fatalf("spec-only edit sent read_only: %v", *body)
	}
}

func TestMCPAddTaskForwardsReadOnly(t *testing.T) {
	s, body, path := readOnlyLoopback(t)
	if _, err := s.handleAutonomousPRDAddTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "story_id": "s1", "title": "Run the gate", "read_only": true,
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.HasSuffix(*path, "/p1/add_task") {
		t.Fatalf("path = %q", *path)
	}
	if v, _ := (*body)["read_only"].(bool); !v {
		t.Fatalf("body = %v, want read_only true", *body)
	}
	_, _ = s.handleAutonomousPRDAddTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "story_id": "s1", "title": "Write code",
	}))
	if v, _ := (*body)["read_only"].(bool); v {
		t.Fatalf("task added without read_only was sent as read_only: %v", *body)
	}
}

func TestMCPTaskToolsDeclareReadOnly(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]mcpsdk.Tool{
		"autonomous_prd_edit_task": s.toolAutonomousPRDEditTask(),
		"autonomous_prd_add_task":  s.toolAutonomousPRDAddTask(),
	} {
		prop, ok := tool.InputSchema.Properties["read_only"].(map[string]any)
		if !ok {
			t.Errorf("%s has no read_only parameter", name)
			continue
		}
		if prop["type"] != "boolean" {
			t.Errorf("%s read_only type = %v, want boolean", name, prop["type"])
		}
	}
	for _, r := range s.toolAutonomousPRDEditTask().InputSchema.Required {
		if r == "new_spec" {
			t.Error("new_spec must be optional so read_only can be edited alone")
		}
	}
}
