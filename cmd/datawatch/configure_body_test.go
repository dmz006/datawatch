package main

import (
	"encoding/json"
	"testing"
)

func TestConfigureRequestBody(t *testing.T) {
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.progress.lanes", "7", float64(7)},
		{"autonomous.progress.inactive_gap_hours", "1.5", 1.5},
		{"autonomous.progress.since", "2026-01-01", "2026-01-01"},
		{"autonomous.progress.backlog_path", "2026/plans/README.md", "2026/plans/README.md"},
		{"autonomous.progress.backlog_path", `docs/"odd"/README.md`, `docs/"odd"/README.md`},
		{"session.auto_git_commit", "true", true},
		{"session.llm_backend", "claude-code", "claude-code"},
		{"autonomous.progress.lanes", "5m", "5m"},
	}
	for _, c := range cases {
		body := configureRequestBody(c.key, c.value)
		var got map[string]any
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Errorf("%s=%s: body %q is not JSON: %v", c.key, c.value, body, err)
			continue
		}
		if got[c.key] != c.want {
			t.Errorf("%s=%s: body %q decoded to %#v, want %#v", c.key, c.value, body, got[c.key], c.want)
		}
	}
}
