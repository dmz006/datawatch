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
	for _, k := range []string{"parallel_read_only_tasks", "parallel_worktree_per_task", "parallel_merge_conflict_policy", "parallel_merge_conflict_model"} {
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
