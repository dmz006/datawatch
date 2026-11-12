// MCP autonomous_prd_set_git — push_on_complete is forwarded only when the
// caller supplied it, so an empty string still means "clear the override /
// inherit the daemon setting" while an omitted parameter does not.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func captureSetGitBody(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	var body []byte
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousPRDSetGit(context.Background(), callReq(args)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	return got
}

func TestMCPSetGitPushOnComplete_ForwardedWhenSupplied(t *testing.T) {
	got := captureSetGitBody(t, map[string]any{
		"id": "p1", "auto_pr": false, "base_branch": "", "push_on_complete": "default_branch",
	})
	if _, ok := got["push_on_complete"]; !ok {
		t.Fatalf("body missing push_on_complete: %v", got)
	}
	if got["push_on_complete"] != "default_branch" {
		t.Errorf("push_on_complete = %v, want default_branch", got["push_on_complete"])
	}
	if got["auto_pr"] != false || got["base_branch"] != "" {
		t.Errorf("existing fields altered: %v", got)
	}
}

func TestMCPSetGitPushOnComplete_EmptyStillForwarded(t *testing.T) {
	got := captureSetGitBody(t, map[string]any{
		"id": "p1", "auto_pr": true, "push_on_complete": "",
	})
	if _, ok := got["push_on_complete"]; !ok {
		t.Fatalf("explicit empty push_on_complete must be forwarded to clear the override: %v", got)
	}
	if got["push_on_complete"] != "" {
		t.Errorf("push_on_complete = %v, want \"\"", got["push_on_complete"])
	}
}

func TestMCPSetGitPushOnComplete_OmittedNotForwarded(t *testing.T) {
	got := captureSetGitBody(t, map[string]any{"id": "p1", "auto_pr": true, "base_branch": "main"})
	if _, ok := got["push_on_complete"]; ok {
		t.Errorf("push_on_complete must be absent when omitted, got %v", got)
	}
}
