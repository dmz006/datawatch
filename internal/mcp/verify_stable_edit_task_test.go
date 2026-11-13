package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyStable_MCPEditTaskForwardsScriptAndReasonOnlyWhenGiven(t *testing.T) {
	s, body, path := readOnlyLoopback(t)
	if _, err := s.handleAutonomousPRDEditTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "task_id": "t1",
		"verification_script": "scripts/verify.sh", "verification_script_reason": "docs only",
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if *path != "/api/autonomous/prds/p1/edit_task" {
		t.Fatalf("path = %q", *path)
	}
	if (*body)["verification_script"] != "scripts/verify.sh" || (*body)["verification_script_reason"] != "docs only" {
		t.Fatalf("body = %v, want both keys", *body)
	}
	// Omitted (and empty) values are not sent, so existing values stay put.
	_, _ = s.handleAutonomousPRDEditTask(context.Background(), toolReq(map[string]any{
		"id": "p1", "task_id": "t1", "new_spec": "reworded", "verification_script": "",
	}))
	for _, k := range []string{"verification_script", "verification_script_reason"} {
		if _, present := (*body)[k]; present {
			t.Fatalf("spec-only edit sent %s: %v", k, *body)
		}
	}
}

func TestVerifyStable_MCPEditTaskToolDeclaresBothParams(t *testing.T) {
	s := &Server{}
	props := s.toolAutonomousPRDEditTask().InputSchema.Properties
	for _, k := range []string{"verification_script", "verification_script_reason"} {
		p, ok := props[k].(map[string]any)
		if !ok {
			t.Fatalf("tool has no %s param", k)
		}
		if d, _ := p["description"].(string); strings.TrimSpace(d) == "" {
			t.Fatalf("%s has no description", k)
		}
		if p["type"] != "string" {
			t.Fatalf("%s type = %v, want string", k, p["type"])
		}
	}
}
