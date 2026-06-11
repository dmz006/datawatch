// BL347 — Session lineage: command parser tests for parent= and kill_children=.

package router

import "testing"

func TestParse_NewWithParent(t *testing.T) {
	cmd := Parse("new: parent=host-abc123: run sub-task")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want CmdNew", cmd.Type)
	}
	if cmd.ParentID != "host-abc123" {
		t.Errorf("ParentID = %q, want host-abc123", cmd.ParentID)
	}
	if cmd.Text != "run sub-task" {
		t.Errorf("Text = %q, want run sub-task", cmd.Text)
	}
	if cmd.KillChildren {
		t.Error("KillChildren should be false by default")
	}
}

func TestParse_NewWithKillChildren(t *testing.T) {
	cmd := Parse("new: kill_children=true: orchestrate something")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want CmdNew", cmd.Type)
	}
	if !cmd.KillChildren {
		t.Error("KillChildren should be true")
	}
	if cmd.Text != "orchestrate something" {
		t.Errorf("Text = %q, want orchestrate something", cmd.Text)
	}
}

func TestParse_NewWithParentAndKillChildren(t *testing.T) {
	cmd := Parse("new: parent=host-pp99 kill_children=true: coordinated sub-task")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want CmdNew", cmd.Type)
	}
	if cmd.ParentID != "host-pp99" {
		t.Errorf("ParentID = %q, want host-pp99", cmd.ParentID)
	}
	if !cmd.KillChildren {
		t.Error("KillChildren should be true")
	}
	if cmd.Text != "coordinated sub-task" {
		t.Errorf("Text = %q, want coordinated sub-task", cmd.Text)
	}
}

func TestParse_NewWithParentAndLLM(t *testing.T) {
	cmd := Parse("new: llm=claude parent=host-zz11: sub-task with llm")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want CmdNew", cmd.Type)
	}
	if cmd.LLMRef != "claude" {
		t.Errorf("LLMRef = %q, want claude", cmd.LLMRef)
	}
	if cmd.ParentID != "host-zz11" {
		t.Errorf("ParentID = %q, want host-zz11", cmd.ParentID)
	}
	if cmd.Text != "sub-task with llm" {
		t.Errorf("Text = %q, want sub-task with llm", cmd.Text)
	}
}

func TestParse_NewKillChildrenFalseExplicit(t *testing.T) {
	cmd := Parse("new: parent=host-aa01 kill_children=false: child task")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want CmdNew", cmd.Type)
	}
	if cmd.ParentID != "host-aa01" {
		t.Errorf("ParentID = %q, want host-aa01", cmd.ParentID)
	}
	if cmd.KillChildren {
		t.Error("KillChildren should be false when explicitly set to false")
	}
}

func TestParse_NewUnknownTokenFallthrough(t *testing.T) {
	// An unknown token like "foo=bar" should fall through to plain text path.
	cmd := Parse("new: foo=bar: some task")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want CmdNew", cmd.Type)
	}
	// Falls through to bare text — entire "foo=bar: some task" is the text.
	if cmd.ParentID != "" {
		t.Errorf("ParentID should be empty for unknown token, got %q", cmd.ParentID)
	}
}
