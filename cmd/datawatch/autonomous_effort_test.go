package main

import (
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
		t.Errorf("auto_git_commit set without ForceAutoGitCommit")
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
	req.ForceAutoGitCommit = true
	body = buildAutonomousSessionBody(req, "spec", "")
	if _, ok := body["claude_effort"]; ok {
		t.Errorf("empty effort produced claude_effort: %v", body["claude_effort"])
	}
	if body["auto_git_commit"] != true {
		t.Errorf("auto_git_commit = %v, want true", body["auto_git_commit"])
	}
}
