package router

import "testing"

// TestParse_NewGitToken verifies "new: git=true: <task>" parses the git flag,
// and that without it the flag defaults to false.
func TestParse_NewGitToken(t *testing.T) {
	cmd := Parse("new: git=true: write the tests")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want %q", cmd.Type, CmdNew)
	}
	if !cmd.Git {
		t.Errorf("Git = %v, want true", cmd.Git)
	}
	if cmd.Text != "write the tests" {
		t.Errorf("Text = %q, want %q", cmd.Text, "write the tests")
	}

	cmd = Parse("new: git=false: write the tests")
	if cmd.Git {
		t.Errorf("explicit git=false should leave Git=false, got true")
	}

	cmd = Parse("new: write the tests")
	if cmd.Git {
		t.Errorf("Git should default to false when git= is omitted, got true")
	}
}

// TestParse_NewGitWithLLM verifies git combines with other new: tokens.
func TestParse_NewGitWithLLM(t *testing.T) {
	cmd := Parse("new: llm=gpt5 git=true: refactor auth")
	if cmd.Type != CmdNew {
		t.Fatalf("Type = %q, want %q", cmd.Type, CmdNew)
	}
	if cmd.LLMRef != "gpt5" {
		t.Errorf("LLMRef = %q, want %q", cmd.LLMRef, "gpt5")
	}
	if !cmd.Git {
		t.Errorf("Git = %v, want true", cmd.Git)
	}
	if cmd.Text != "refactor auth" {
		t.Errorf("Text = %q, want %q", cmd.Text, "refactor auth")
	}
}

// TestParse_SpawnGitToken verifies the git= key on the "schedule spawn" verb.
func TestParse_SpawnGitToken(t *testing.T) {
	cmd := parseScheduleSpawnParams("task=clean up dir=/tmp git=true")
	if cmd.Type != CmdScheduleSpawn {
		t.Fatalf("Type = %q, want %q", cmd.Type, CmdScheduleSpawn)
	}
	if !cmd.SpawnGit {
		t.Errorf("SpawnGit = %v, want true", cmd.SpawnGit)
	}
	if cmd.SpawnTask != "clean up" {
		t.Errorf("SpawnTask = %q, want %q", cmd.SpawnTask, "clean up")
	}
	if cmd.SpawnDir != "/tmp" {
		t.Errorf("SpawnDir = %q, want %q", cmd.SpawnDir, "/tmp")
	}

	cmd = parseScheduleSpawnParams("task=clean up dir=/tmp")
	if cmd.SpawnGit {
		t.Errorf("SpawnGit should default to false when git= is omitted, got true")
	}
}
