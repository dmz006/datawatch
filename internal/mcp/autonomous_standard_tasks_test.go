// Tests for the autonomous_standard_tasks MCP tool, proxied against an
// httptest backend that stands in for GET /api/autonomous/standard-tasks.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestStandardTaskMCPTool(t *testing.T) {
	const reply = `{"enabled":true,"tasks":[{"id":"changelog_entry","title":"Changelog entry","kind":"docs","summary":"Adds a changelog entry.","offered_when":"always","enabled":true,"project_override":null,"mode":"model session"}]}`
	s, rec := activateServer(t, http.StatusOK, reply)

	if got := s.toolAutonomousStandardTasks().Name; got != "autonomous_standard_tasks" {
		t.Fatalf("tool name = %q", got)
	}

	res, err := s.handleAutonomousStandardTasks(context.Background(), callReq(map[string]any{"project": "/work/app"}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodGet || rec.path != "/api/autonomous/standard-tasks" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}

	text := res.Content[0].(mcpsdk.TextContent).Text
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("reply is not JSON: %v", err)
	}
	for _, k := range []string{"enabled", "tasks"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("reply missing top-level field %q", k)
		}
	}
	rows, _ := obj["tasks"].([]any)
	if len(rows) != 1 {
		t.Fatalf("tasks = %v", obj["tasks"])
	}
	row, _ := rows[0].(map[string]any)
	for _, k := range []string{"id", "title", "kind", "summary", "offered_when", "enabled", "project_override", "mode"} {
		if _, ok := row[k]; !ok {
			t.Errorf("task row missing field %q", k)
		}
	}
}
