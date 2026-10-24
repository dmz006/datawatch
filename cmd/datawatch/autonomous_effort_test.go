package main

import (
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestMapAutonomousEffortToSession(t *testing.T) {
	// Pre-existing (v5.26.11) outputs — must not change.
	cases := []struct {
		in   autonomouspkg.Effort
		want string
	}{
		{autonomouspkg.EffortLow, "quick"},
		{autonomouspkg.EffortQuick, "quick"},
		{autonomouspkg.EffortMedium, "normal"},
		{autonomouspkg.EffortNormal, "normal"},
		{autonomouspkg.EffortHigh, "thorough"},
		{autonomouspkg.EffortMax, "thorough"},
		{autonomouspkg.EffortThorough, "thorough"},
		{"xhigh", ""},
		{"", ""},
		{"bogus", ""},
	}
	for _, c := range cases {
		if got := mapAutonomousEffortToSession(c.in); got != c.want {
			t.Errorf("mapAutonomousEffortToSession(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMapAutonomousEffortToClaude(t *testing.T) {
	cases := []struct {
		in   autonomouspkg.Effort
		want string
	}{
		{autonomouspkg.EffortLow, "low"},
		{autonomouspkg.EffortMedium, "medium"},
		{autonomouspkg.EffortHigh, "high"},
		{autonomouspkg.EffortMax, "max"},
		{"xhigh", "xhigh"},
		{autonomouspkg.EffortQuick, "low"},
		{autonomouspkg.EffortNormal, "medium"},
		{autonomouspkg.EffortThorough, "high"},
		{"", ""},
		{"bogus", ""},
	}
	for _, c := range cases {
		if got := mapAutonomousEffortToClaude(c.in); got != c.want {
			t.Errorf("mapAutonomousEffortToClaude(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAutonomousClaudeEffortApplies(t *testing.T) {
	cases := []struct {
		req, def string
		want     bool
	}{
		{"claude-code", "", true},
		{"claude-code", "opencode", true},
		{"", "", true},
		{"  ", "", true},
		{"", "claude-code", true},
		{"opencode", "", false},
		{"ollama", "claude-code", false},
		{"goose", "", false},
		{"", "opencode", false},
	}
	for _, c := range cases {
		if got := autonomousClaudeEffortApplies(c.req, c.def); got != c.want {
			t.Errorf("autonomousClaudeEffortApplies(%q, %q) = %v, want %v", c.req, c.def, got, c.want)
		}
	}
}

func TestBuildAutonomousSessionBody(t *testing.T) {
	req := autonomouspkg.SpawnRequest{
		Title:   "t",
		Backend: "claude-code",
		Effort:  autonomouspkg.EffortLow,
	}
	body := buildAutonomousSessionBody(req, "spec", "")
	if body["effort"] != "quick" {
		t.Errorf("effort = %v, want quick", body["effort"])
	}
	if body["claude_effort"] != "low" {
		t.Errorf("claude_effort = %v, want low", body["claude_effort"])
	}
	if body["task"] != "spec" || body["name"] != "autonomous:t" || body["one_shot"] != true {
		t.Errorf("unexpected base keys: %v", body)
	}
	if _, ok := body["auto_git_commit"]; ok {
		t.Errorf("auto_git_commit should not be set: %v", body["auto_git_commit"])
	}

	// No git settings → no git key (existing flow unchanged).
	if _, ok := body["git"]; ok {
		t.Errorf("git key must be absent when the PRD has no git settings: %v", body["git"])
	}

	// Git settings present on the PRD → git:true forwarded so the session
	// manager opts into git activity (pre-session snapshot + completion
	// commit), because PRDs are development work.
	reqWithGit := req
	reqWithGit.Git = true
	bodyWithGit := buildAutonomousSessionBody(reqWithGit, "spec", "")
	if bodyWithGit["git"] != true {
		t.Errorf("git key = %v, want true when SpawnRequest.Git is set", bodyWithGit["git"])
	}

	req.Backend = "opencode"
	body = buildAutonomousSessionBody(req, "spec", "claude-code")
	if body["effort"] != "quick" {
		t.Errorf("opencode effort = %v, want quick", body["effort"])
	}
	if _, ok := body["claude_effort"]; ok {
		t.Errorf("opencode body has claude_effort: %v", body["claude_effort"])
	}

	// Empty effort → no claude_effort key even on claude-code.
	req.Backend = ""
	req.Effort = ""
	body = buildAutonomousSessionBody(req, "spec", "")
	if _, ok := body["claude_effort"]; ok {
		t.Errorf("empty effort produced claude_effort: %v", body["claude_effort"])
	}
	if _, ok := body["auto_git_commit"]; ok {
		t.Errorf("auto_git_commit should not be set: %v", body["auto_git_commit"])
	}
}

func TestIsLightweightHarnessKind(t *testing.T) {
	cases := map[string]bool{
		"opencode":     true,
		"opencode-acp": true,
		"claude-code":  false,
		"ollama":       false,
		"":             false,
	}
	for kind, want := range cases {
		if got := isLightweightHarnessKind(kind); got != want {
			t.Errorf("isLightweightHarnessKind(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestBuildLightweightOrFullHarnessBlock(t *testing.T) {
	full := buildLightweightOrFullHarnessBlock(false)
	if !strings.Contains(full, "CHECKPOINT PROTOCOL") {
		t.Errorf("full block missing CHECKPOINT PROTOCOL section: %s", full)
	}
	light := buildLightweightOrFullHarnessBlock(true)
	if strings.Contains(light, "CHECKPOINT PROTOCOL") || strings.Contains(light, "BEFORE YOU FINISH") {
		t.Errorf("lightweight block should not contain the long-form headings, got: %s", light)
	}
	if strings.Contains(full, "BEFORE YOU FINISH") {
		t.Errorf("full block should no longer contain the removed self-check section, got: %s", full)
	}
	if !strings.Contains(light, "CHECKPOINT.md") {
		t.Errorf("lightweight block should still mention CHECKPOINT.md, got: %s", light)
	}
	if len(light) >= len(full) {
		t.Errorf("lightweight block (%d bytes) should be shorter than full block (%d bytes)", len(light), len(full))
	}
}

func TestHarnessBlockCommitInstructions(t *testing.T) {
	blocks := map[string]string{
		"full":        buildLightweightOrFullHarnessBlock(false),
		"lightweight": buildLightweightOrFullHarnessBlock(true),
	}
	for name, block := range blocks {
		lower := strings.ToLower(block)
		if !strings.Contains(lower, "do not run git") {
			t.Errorf("%s block missing 'do not run git' instruction, got: %s", name, block)
		}
		if !strings.Contains(lower, "datawatch_complete: <one-line plain-text summary of what you did>") {
			t.Errorf("%s block missing DATAWATCH_COMPLETE one-line summary instruction, got: %s", name, block)
		}
		// 'git commit' may only appear inside the negation "do not run git
		// add or git commit", never as a command the worker should execute.
		if strings.Contains(lower, "run `git") || strings.Contains(lower, "git log") ||
			strings.Contains(lower, "git status") || strings.Contains(lower, "stage and commit") ||
			strings.Contains(lower, "commit your work") {
			t.Errorf("%s block still instructs the worker to run git commands, got: %s", name, block)
		}
	}
}

func TestAutonomousSessionBodyCarriesTaskTitle(t *testing.T) {
	req := autonomouspkg.SpawnRequest{Title: "Add retry to fetcher"}
	body := buildAutonomousSessionBody(req, "spec", "")
	if got := body["task_title"]; got != "Add retry to fetcher" {
		t.Errorf("task_title = %v, want %q", got, "Add retry to fetcher")
	}
	if body["name"] != "autonomous:Add retry to fetcher" {
		t.Errorf("name = %v, want the autonomous: prefixed title", body["name"])
	}

	req.Title = ""
	body = buildAutonomousSessionBody(req, "spec", "")
	if _, ok := body["task_title"]; ok {
		t.Errorf("task_title should be absent when Title is empty, got %v", body["task_title"])
	}
}

func TestBuildAutonomousSessionBodyCarriesWriteScope(t *testing.T) {
	req := autonomouspkg.SpawnRequest{WriteScope: []string{"/a", "/b"}}
	body := buildAutonomousSessionBody(req, "spec", "")
	got, ok := body["write_scope"].([]string)
	if !ok {
		t.Fatalf("write_scope = %T, want []string", body["write_scope"])
	}
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Errorf("write_scope = %v, want [/a /b] in order", got)
	}

	reqEmpty := autonomouspkg.SpawnRequest{}
	bodyEmpty := buildAutonomousSessionBody(reqEmpty, "spec", "")
	if _, ok := bodyEmpty["write_scope"]; ok {
		t.Errorf("write_scope should be absent when WriteScope is empty, got %v", bodyEmpty["write_scope"])
	}
}
