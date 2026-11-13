package mcp

import (
	"context"
	"testing"
)

// TestAutonomousListFull_Schema: the list tool declares full, limit and cursor;
// the tasks tool requires id and declares the paging arguments.
func TestAutonomousListFull_Schema(t *testing.T) {
	s := &Server{}
	list := s.toolAutonomousPRDList()
	for _, k := range []string{"full", "limit", "cursor", "status", "shipped"} {
		if _, ok := list.InputSchema.Properties[k]; !ok {
			t.Errorf("autonomous_prd_list missing %q", k)
		}
	}
	if p, _ := list.InputSchema.Properties["full"].(map[string]any); p["type"] != "boolean" {
		t.Errorf("full type = %v; want boolean", p["type"])
	}
	tasks := s.toolAutonomousPRDTasks()
	for _, k := range []string{"id", "limit", "cursor", "story", "status"} {
		if _, ok := tasks.InputSchema.Properties[k]; !ok {
			t.Errorf("autonomous_prd_tasks missing %q", k)
		}
	}
	if len(tasks.InputSchema.Required) != 1 || tasks.InputSchema.Required[0] != "id" {
		t.Errorf("required = %v; want [id]", tasks.InputSchema.Required)
	}
}

// TestAutonomousListFull_Handler: default sends no full flag; full=true,
// limit and cursor are forwarded; the tasks handler builds the paged path.
func TestAutonomousListFull_Handler(t *testing.T) {
	var calls []shipCall
	s := capacityTestServer(t, shipCountsHandler(&calls))
	ctx := context.Background()

	if _, err := s.handleAutonomousPRDList(ctx, callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].path != "/api/autonomous/prds" || calls[0].query != "" {
		t.Fatalf("default list = %+v; want bare /api/autonomous/prds", calls)
	}

	calls = nil
	if _, err := s.handleAutonomousPRDList(ctx, callReq(map[string]any{"full": true, "limit": 5, "cursor": "abc"})); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].query != "cursor=abc&full=true&limit=5" {
		t.Fatalf("full list = %+v", calls)
	}

	calls = nil
	res, err := s.handleAutonomousPRDList(ctx, callReq(map[string]any{"limit": -3}))
	if err != nil || len(calls) != 0 || !res.IsError {
		t.Errorf("negative limit: err=%v calls=%d isError=%v; want a tool error and no request", err, len(calls), res != nil && res.IsError)
	}

	calls = nil
	if _, err := s.handleAutonomousPRDTasks(ctx, callReq(map[string]any{"id": "cc29", "limit": 10, "cursor": "Zm9v", "status": "failed"})); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].path != "/api/autonomous/prds/cc29/tasks" || calls[0].query != "cursor=Zm9v&limit=10&status=failed" {
		t.Fatalf("tasks = %+v", calls)
	}

	calls = nil
	res, err = s.handleAutonomousPRDTasks(ctx, callReq(map[string]any{}))
	if err != nil || len(calls) != 0 || !res.IsError {
		t.Errorf("missing id: want a tool error and no request")
	}
}
