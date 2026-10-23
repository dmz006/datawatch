// MCP tests for automation_set_decision_assistant, proxied against an httptest
// backend.

package mcp

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestAutomationSetDecisionAssistantTool(t *testing.T) {
	def := (&Server{}).toolAutomationSetDecisionAssistant()
	if def.Name != "automation_set_decision_assistant" {
		t.Fatalf("tool name = %q", def.Name)
	}
	if len(def.InputSchema.Required) != 1 || def.InputSchema.Required[0] != "automation_id" {
		t.Errorf("required = %v, want only automation_id", def.InputSchema.Required)
	}
	for _, k := range []string{"member_id", "backend", "model", "mode", "hooks", "thresholds", "clear", "expected_version", "actor", "reason"} {
		if _, ok := def.InputSchema.Properties[k]; !ok {
			t.Errorf("schema lacks %s", k)
		}
	}
	if got, ok := federation.MCPToolCap["automation_set_decision_assistant"]; !ok || got != federation.CapAutonomousWrite {
		t.Errorf("federation capability = %v (present=%v), want CapAutonomousWrite", got, ok)
	}
	var listed bool
	for _, n := range AutomationToolNames() {
		listed = listed || n == "automation_set_decision_assistant"
	}
	if !listed {
		t.Error("AutomationToolNames lacks automation_set_decision_assistant")
	}
}

func TestAutomationSetDecisionAssistantProxies(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"ok"}`)
	res, err := s.handleAutomationSetDecisionAssistant(context.Background(), callReq(map[string]any{
		"automation_id": "ab12", "expected_version": float64(3), "actor": "alice", "reason": "because",
		"member_id": "m1", "backend": "pi-prompt", "model": "qwen3.8:27b", "mode": "live",
		"hooks":      []any{"empty_edge", "skip_member"},
		"thresholds": map[string]any{"empty_edge": 0.8},
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/automations/ab12/set_decision_assistant" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	want := map[string]any{
		"expected_version": float64(3), "actor": "alice", "reason": "because",
		"member_id": "m1", "backend": "pi-prompt", "model": "qwen3.8:27b", "mode": "live",
		"hooks":      []any{"empty_edge", "skip_member"},
		"thresholds": map[string]any{"empty_edge": 0.8},
	}
	if !reflect.DeepEqual(rec.body, want) {
		t.Errorf("body = %v, want %v", rec.body, want)
	}
	if text := automationText(t, res); !strings.Contains(text, `"ok"`) {
		t.Errorf("result = %s", text)
	}

	// An empty backend is forwarded so it turns the assistant off.
	if _, err := s.handleAutomationSetDecisionAssistant(context.Background(), callReq(map[string]any{"automation_id": "ab12", "backend": ""})); err != nil {
		t.Fatal(err)
	}
	if v, ok := rec.body["backend"]; !ok || v != "" || len(rec.body) != 1 {
		t.Errorf("empty backend body = %v", rec.body)
	}
	if _, err := s.handleAutomationSetDecisionAssistant(context.Background(), callReq(map[string]any{"automation_id": "ab12", "clear": true})); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.body, map[string]any{"clear": true}) {
		t.Errorf("clear body = %v", rec.body)
	}
}

func TestAutomationSetDecisionAssistantArgErrors(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	for name, args := range map[string]map[string]any{
		"no id":           {"backend": "x"},
		"hooks not array": {"automation_id": "ab12", "hooks": "empty_edge"},
		"bad hook item":   {"automation_id": "ab12", "hooks": []any{1}},
		"bad threshold":   {"automation_id": "ab12", "thresholds": map[string]any{"empty_edge": "high"}},
		"thresholds type": {"automation_id": "ab12", "thresholds": "x"},
		"clear type":      {"automation_id": "ab12", "clear": "yes"},
		"mode type":       {"automation_id": "ab12", "mode": 1},
	} {
		rec.method = ""
		res, err := s.handleAutomationSetDecisionAssistant(context.Background(), callReq(args))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if text := automationText(t, res); !strings.HasPrefix(text, "error:") {
			t.Errorf("%s: result = %q, want an error", name, text)
		}
		if rec.method != "" {
			t.Errorf("%s reached the daemon", name)
		}
	}
}

func TestAutomationSetDecisionAssistantDaemonError(t *testing.T) {
	s, _ := activateServer(t, http.StatusBadRequest, `decision assistant: unknown hook "x"`)
	res, err := s.handleAutomationSetDecisionAssistant(context.Background(), callReq(map[string]any{"automation_id": "ab12", "backend": "b", "model": "m"}))
	if err != nil {
		t.Fatal(err)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "unknown hook") {
		t.Errorf("result = %q", text)
	}
}
