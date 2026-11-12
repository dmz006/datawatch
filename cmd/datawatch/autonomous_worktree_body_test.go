package main

import (
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// A worker spawned into an Automaton-owned worktree asks the session manager
// to refuse a missing or non-worktree directory; every other launch is
// unchanged.
func TestAutonomousSessionBody_RequireExistingWorktree(t *testing.T) {
	owned := buildAutonomousSessionBody(autonomouspkg.SpawnRequest{Title: "t", SkipDirtyCapture: true}, "spec", "")
	if owned["require_existing_worktree"] != true {
		t.Errorf("require_existing_worktree = %v, want true for a worktree-owned spawn", owned["require_existing_worktree"])
	}
	plain := buildAutonomousSessionBody(autonomouspkg.SpawnRequest{Title: "t"}, "spec", "")
	if _, ok := plain["require_existing_worktree"]; ok {
		t.Errorf("require_existing_worktree must be absent for a non-worktree spawn, got %v", plain["require_existing_worktree"])
	}
}
