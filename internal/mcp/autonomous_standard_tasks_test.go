// Tests for the autonomous_standard_tasks MCP tool, proxied against an
// httptest backend that stands in for GET /api/autonomous/standard-tasks.
// The backend replies with the shared fixture, so the tool's output is
// compared against it.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// standardTasksFixturePath is the shared expected list every surface reads.
const standardTasksFixturePath = "../../testdata/standard-tasks/expected.json"

func TestStandardTaskMCPTool(t *testing.T) {
	fixture, err := os.ReadFile(standardTasksFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var want struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	s, rec := activateServer(t, http.StatusOK, string(fixture))

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
	if len(rows) != len(want.Tasks) {
		t.Fatalf("tasks = %d, fixture = %d", len(rows), len(want.Tasks))
	}
	// The tool prints the fixture's fields; each must match the fixture row.
	for i, w := range want.Tasks {
		row, _ := rows[i].(map[string]any)
		for _, k := range []string{"id", "title", "kind", "enabled", "mode"} {
			if _, ok := row[k]; !ok {
				t.Errorf("task row %d missing field %q", i, k)
				continue
			}
			if row[k] != w[k] {
				t.Errorf("task row %d %s = %v, want %v", i, k, row[k], w[k])
			}
		}
	}
}
