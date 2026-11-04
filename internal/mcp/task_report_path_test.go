package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const mcpTestReportPath = ".datawatch/reports/gate.md"

func TestMCPEditTaskForwardsReportPath(t *testing.T) {
	s, body, _ := readOnlyLoopback(t)
	if _, err := s.handleAutonomousPRDEditTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "task_id": "t1", "read_only": true, "report_path": mcpTestReportPath,
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if v, _ := (*body)["report_path"].(string); v != mcpTestReportPath {
		t.Fatalf("body report_path = %v, want %q (body %v)", (*body)["report_path"], mcpTestReportPath, *body)
	}
	// Omitted → not in the body, so an existing path stays put.
	_, _ = s.handleAutonomousPRDEditTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "task_id": "t1", "new_spec": "reworded",
	}))
	if _, present := (*body)["report_path"]; present {
		t.Fatalf("spec-only edit sent report_path: %v", *body)
	}
}

func TestMCPAddTaskForwardsReportPath(t *testing.T) {
	s, body, _ := readOnlyLoopback(t)
	if _, err := s.handleAutonomousPRDAddTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "story_id": "s1", "title": "Run the gate", "read_only": true, "report_path": mcpTestReportPath,
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if v, _ := (*body)["report_path"].(string); v != mcpTestReportPath {
		t.Fatalf("body report_path = %v, want %q (body %v)", (*body)["report_path"], mcpTestReportPath, *body)
	}
	// Omitted → not in the body.
	_, _ = s.handleAutonomousPRDAddTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "story_id": "s1", "title": "Write code",
	}))
	if _, present := (*body)["report_path"]; present {
		t.Fatalf("plain add sent report_path: %v", *body)
	}
}

func TestMCPTaskToolsDeclareReportPath(t *testing.T) {
	s := &Server{}
	for name, tool := range map[string]mcpsdk.Tool{
		"autonomous_prd_edit_task": s.toolAutonomousPRDEditTask(),
		"autonomous_prd_add_task":  s.toolAutonomousPRDAddTask(),
	} {
		prop, ok := tool.InputSchema.Properties["report_path"].(map[string]any)
		if !ok {
			t.Errorf("%s has no report_path parameter", name)
			continue
		}
		if prop["type"] != "string" {
			t.Errorf("%s report_path type = %v, want string", name, prop["type"])
		}
		if !strings.Contains(prop["description"].(string), ".datawatch/reports/") {
			t.Errorf("%s report_path description should name .datawatch/reports/: %v", name, prop["description"])
		}
	}
}
