package mcp

import (
	"strings"
	"testing"
)

func TestParallelConfigArgsPresenceAware(t *testing.T) {
	got := parallelConfigArgs(toolReq(map[string]any{
		"parallel_worktree_per_task":     false,
		"parallel_merge_conflict_policy": "block",
		"parallel_merge_conflict_model":  "",
	}))
	if v, ok := got["autonomous.parallel.worktree_per_task"].(bool); !ok || v {
		t.Fatalf("worktree_per_task = %v, want explicit false", got["autonomous.parallel.worktree_per_task"])
	}
	if got["autonomous.parallel.merge_conflict_policy"] != "block" {
		t.Fatalf("policy = %v", got["autonomous.parallel.merge_conflict_policy"])
	}
	if v, ok := got["autonomous.parallel.merge_conflict_model"]; !ok || v != "" {
		t.Fatalf("model = %v, want explicit empty string", v)
	}
	if got["autonomous.parallel.merge_retry_seconds"] != nil {
		t.Fatalf("absent merge_retry_seconds must not be sent: %v", got)
	}
	if _, ok := got["autonomous.parallel.read_only_tasks"]; ok {
		t.Fatalf("absent argument must not be sent: %v", got)
	}
	if n := len(parallelConfigArgs(toolReq(map[string]any{}))); n != 0 {
		t.Fatalf("empty request sent %d entries", n)
	}
}

func TestAutonomousConfigSetDeclaresParallelArguments(t *testing.T) {
	s := &Server{}
	props := s.toolAutonomousConfigSet().InputSchema.Properties
	for _, k := range []string{"parallel_read_only_tasks", "parallel_worktree_per_task", "parallel_merge_conflict_policy", "parallel_merge_conflict_model", "parallel_merge_retry_seconds"} {
		if _, ok := props[k]; !ok {
			t.Errorf("autonomous_config_set lacks %s", k)
		}
	}
}

func TestAutonomousConfigSetSendsParallelKeys(t *testing.T) {
	s, body, path := readOnlyLoopback(t)
	if _, err := s.handleAutonomousConfigSet(nil, toolReq(map[string]any{ //nolint:staticcheck
		"parallel_merge_conflict_policy": "triage",
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.HasSuffix(*path, "/api/config") {
		t.Fatalf("path = %q, want the config route", *path)
	}
	if (*body)["autonomous.parallel.merge_conflict_policy"] != "triage" {
		t.Fatalf("body = %v", *body)
	}
}

func TestParallelConfigArgsMergeRetrySeconds(t *testing.T) {
	// set: an explicit 0 (timer off) is sent, not dropped
	got := parallelConfigArgs(toolReq(map[string]any{"parallel_merge_retry_seconds": float64(0)}))
	if v, ok := got["autonomous.parallel.merge_retry_seconds"]; !ok || v != float64(0) {
		t.Fatalf("merge_retry_seconds = %v, want explicit 0", v)
	}
	// default: absent sends nothing
	if n := len(parallelConfigArgs(toolReq(map[string]any{}))); n != 0 {
		t.Fatalf("empty request sent %d entries", n)
	}
	// invalid: the tool schema bounds the value at 0 so a negative is refused
	s := &Server{}
	prop, ok := s.toolAutonomousConfigSet().InputSchema.Properties["parallel_merge_retry_seconds"].(map[string]any)
	if !ok || prop["minimum"] != float64(0) {
		t.Fatalf("schema = %v, want minimum 0", prop)
	}
}
