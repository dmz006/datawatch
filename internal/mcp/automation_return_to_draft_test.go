// automation_return_to_draft tool, proxied against an httptest backend.
// Reuses activateServer/callReq/automationText from automation_activate_test.go.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationReturnToDraftToolName(t *testing.T) {
	if got := (&Server{}).toolAutomationReturnToDraft().Name; got != "automation_return_to_draft" {
		t.Fatalf("tool name = %q", got)
	}
}

func TestAutomationReturnToDraftProxiesPOST(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"draft","version":4}`)
	res, err := s.handleAutomationReturnToDraft(context.Background(), callReq(map[string]any{
		"id": "ab12", "actor": "alice", "expected_version": float64(4),
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/automations/ab12/return_to_draft" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["actor"] != "alice" {
		t.Errorf("body actor = %v, want alice (body %v)", rec.body["actor"], rec.body)
	}
	if got, ok := rec.body["expected_version"].(float64); !ok || got != 4 {
		t.Errorf("body expected_version = %v, want 4 (body %v)", rec.body["expected_version"], rec.body)
	}
	if text := automationText(t, res); !strings.Contains(text, `"draft"`) {
		t.Errorf("result = %s", text)
	}
}

func TestAutomationReturnToDraftOmitsEmptyFieldsAndReportsConflict(t *testing.T) {
	s, rec := activateServer(t, http.StatusConflict, "automation ab12 is active; only a blocked automation with nothing started can go back to draft")
	res, err := s.handleAutomationReturnToDraft(context.Background(), callReq(map[string]any{"id": "ab12"}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if _, ok := rec.body["actor"]; ok {
		t.Errorf("empty actor should not be sent: %v", rec.body)
	}
	if _, ok := rec.body["expected_version"]; ok {
		t.Errorf("absent expected_version should not be sent: %v", rec.body)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "only a blocked") {
		t.Errorf("conflict not surfaced: %s", text)
	}
}

func TestAutomationReturnToDraftRejectsMissingIDAndBadVersion(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if res, err := s.handleAutomationReturnToDraft(context.Background(), callReq(nil)); err != nil || !strings.HasPrefix(automationText(t, res), "error:") || rec.path != "" {
		t.Errorf("missing id: res=%v path=%q, want an error and no request", res, rec.path)
	}
	if res, err := s.handleAutomationReturnToDraft(context.Background(), callReq(map[string]any{"id": "ab12", "expected_version": "four"})); err != nil || !strings.Contains(automationText(t, res), "expected_version") || rec.path != "" {
		t.Errorf("bad version: res=%v path=%q, want an error and no request", res, rec.path)
	}
}
