// REST tests for POST /api/automations/{id}/set_decision_assistant, driven
// through the real autonomous manager: the Automation and member scopes, the
// clear forms, the guards and the audit entry shape.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// decisionAssistantRecord is the decision assistant subset of the Automation
// wire record.
type decisionAssistantRecord struct {
	ID                string                        `json:"id"`
	Version           int64                         `json:"version"`
	DecisionAssistant *autonomous.DecisionAssistant `json:"decision_assistant"`
	Members           []struct {
		ID                string                        `json:"id"`
		DecisionAssistant *autonomous.DecisionAssistant `json:"decision_assistant"`
	} `json:"members"`
}

func decodeDecisionAssistant(t *testing.T, body string) decisionAssistantRecord {
	t.Helper()
	var a decisionAssistantRecord
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("decode automation: %v (body=%q)", err, body)
	}
	return a
}

func newDecisionAssistantServer(t *testing.T) (*Server, *audit.Log, decisionAssistantRecord) {
	t.Helper()
	s, al, base := newAutomationLLMServer(t)
	a := decodeDecisionAssistant(t, doAutomation(t, s, http.MethodGet, "/api/automations/"+base.ID, nil, http.StatusOK))
	return s, al, a
}

func TestAutomationRESTSetDecisionAssistant(t *testing.T) {
	s, al, a := newDecisionAssistantServer(t)
	path := "/api/automations/" + a.ID + "/set_decision_assistant"
	if a.DecisionAssistant != nil {
		t.Fatalf("a new Automation has a decision assistant: %+v", a.DecisionAssistant)
	}

	got := decodeDecisionAssistant(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"expected_version": a.Version, "actor": "alice", "reason": "try it",
		"backend": "claude-code", "model": "claude-haiku-5-5", "mode": "live",
		"hooks":      []string{"empty_edge", "budget_breach"},
		"thresholds": map[string]float64{"empty_edge": 0.8},
	}, http.StatusOK))
	da := got.DecisionAssistant
	if da == nil || da.Backend != "claude-code" || da.Model != "claude-haiku-5-5" || da.Mode != "live" ||
		len(da.Hooks) != 2 || da.Thresholds["empty_edge"] != 0.8 {
		t.Fatalf("set_decision_assistant result = %+v", da)
	}
	if got.Version != a.Version+1 {
		t.Errorf("version = %d, want %d", got.Version, a.Version+1)
	}
	wantAudit(t, al, "automation_set_decision_assistant", a.ID, map[string]any{
		"actor": "alice", "reason": "try it", "expected_version": float64(a.Version),
		"backend": "claude-code", "model": "claude-haiku-5-5", "mode": "live", "cleared": false,
		"version": float64(a.Version + 1),
	})

	// get returns the field.
	read := decodeDecisionAssistant(t, doAutomation(t, s, http.MethodGet, "/api/automations/"+a.ID, nil, http.StatusOK))
	if read.DecisionAssistant == nil || read.DecisionAssistant.Backend != "claude-code" {
		t.Errorf("get lacks the decision assistant: %+v", read.DecisionAssistant)
	}

	// The default mode is shadow and is what the audit entry records.
	doAutomation(t, s, http.MethodPost, path, map[string]any{"backend": "pi-prompt", "model": "qwen3.8:27b"}, http.StatusOK)
	wantAudit(t, al, "automation_set_decision_assistant", a.ID, map[string]any{"backend": "pi-prompt", "mode": "shadow", "cleared": false})

	// An empty backend, and clear=true, turn it off.
	cleared := decodeDecisionAssistant(t, doAutomation(t, s, http.MethodPost, path, map[string]any{"backend": ""}, http.StatusOK))
	if cleared.DecisionAssistant != nil {
		t.Errorf("empty backend did not clear: %+v", cleared.DecisionAssistant)
	}
	doAutomation(t, s, http.MethodPost, path, map[string]any{"backend": "pi-prompt", "model": "qwen3.8:27b"}, http.StatusOK)
	cleared = decodeDecisionAssistant(t, doAutomation(t, s, http.MethodPost, path, map[string]any{"clear": true, "actor": "bob"}, http.StatusOK))
	if cleared.DecisionAssistant != nil {
		t.Errorf("clear=true did not clear: %+v", cleared.DecisionAssistant)
	}
	wantAudit(t, al, "automation_set_decision_assistant", a.ID, map[string]any{"actor": "bob", "cleared": true})
	if n := countAutomationAudit(t, al, "automation_set_decision_assistant"); n != 5 {
		t.Errorf("audit entries = %d, want 5", n)
	}
}

func TestAutomationRESTSetDecisionAssistantMember(t *testing.T) {
	s, al, a := newDecisionAssistantServer(t)
	if len(a.Members) != 1 {
		t.Fatalf("members = %d, want 1", len(a.Members))
	}
	member := a.Members[0].ID
	path := "/api/automations/" + a.ID + "/set_decision_assistant"

	got := decodeDecisionAssistant(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"member_id": member, "backend": "pi-prompt", "model": "qwen3.8:27b", "hooks": []string{"skip_member"},
	}, http.StatusOK))
	if got.DecisionAssistant != nil {
		t.Errorf("a member setting changed the Automation's: %+v", got.DecisionAssistant)
	}
	if got.Members[0].DecisionAssistant == nil || got.Members[0].DecisionAssistant.Backend != "pi-prompt" {
		t.Fatalf("member setting = %+v", got.Members[0].DecisionAssistant)
	}
	wantAudit(t, al, "automation_set_decision_assistant", a.ID, map[string]any{"member_id": member, "backend": "pi-prompt", "cleared": false})

	got = decodeDecisionAssistant(t, doAutomation(t, s, http.MethodPost, path, map[string]any{"member_id": member, "clear": true}, http.StatusOK))
	if got.Members[0].DecisionAssistant != nil {
		t.Errorf("member clear failed: %+v", got.Members[0].DecisionAssistant)
	}
	doAutomation(t, s, http.MethodPost, path, map[string]any{"member_id": "nope", "backend": "pi-prompt", "model": "qwen3.8:27b"}, http.StatusNotFound)
}

func TestAutomationRESTSetDecisionAssistantGuards(t *testing.T) {
	s, al, a := newDecisionAssistantServer(t)
	path := "/api/automations/" + a.ID + "/set_decision_assistant"
	ok := map[string]any{"backend": "pi-prompt", "model": "qwen3.8:27b"}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}

	t.Run("nothing named", func(t *testing.T) {
		out := doAutomation(t, s, http.MethodPost, path, map[string]any{"actor": "x"}, http.StatusBadRequest)
		if !strings.Contains(out, "clear") {
			t.Errorf("message should name the clear form: %q", out)
		}
	})
	t.Run("clear with settings", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, path, with("clear", true), http.StatusBadRequest)
	})
	t.Run("backend without model", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, path, map[string]any{"backend": "pi-prompt"}, http.StatusBadRequest)
	})
	t.Run("unknown backend", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, path, map[string]any{"backend": "no-such-llm", "model": "m"}, http.StatusBadRequest)
	})
	t.Run("bad mode, hook and threshold", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, path, with("mode", "auto"), http.StatusBadRequest)
		doAutomation(t, s, http.MethodPost, path, with("hooks", []string{"nope"}), http.StatusBadRequest)
		doAutomation(t, s, http.MethodPost, path, with("thresholds", map[string]float64{"empty_edge": 1.5}), http.StatusBadRequest)
		doAutomation(t, s, http.MethodPost, path, with("thresholds", map[string]float64{"nope": 0.5}), http.StatusBadRequest)
	})
	t.Run("bad JSON", func(t *testing.T) {
		rr := httptest.NewRecorder()
		s.handleAutomations(rr, httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString("{not json")))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("code = %d, want 400", rr.Code)
		}
	})
	t.Run("stale version", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, path, with("expected_version", a.Version+50), http.StatusConflict)
	})
	t.Run("unknown automation", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, "/api/automations/nope/set_decision_assistant", ok, http.StatusNotFound)
	})
	t.Run("GET is 405", func(t *testing.T) {
		doAutomation(t, s, http.MethodGet, path, nil, http.StatusMethodNotAllowed)
	})
	t.Run("missing write capability", func(t *testing.T) {
		b, _ := json.Marshal(ok)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{"autonomous:read"}))
		rr := httptest.NewRecorder()
		s.handleAutomations(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("code = %d, want 403", rr.Code)
		}
	})
	if n := countAutomationAudit(t, al, "automation_set_decision_assistant"); n != 0 {
		t.Errorf("refused calls wrote %d audit entries, want 0", n)
	}
}

// TestAutomationDecisionAssistantAuditShape checks the audit entry carries the
// resource type and id the audit rule requires.
func TestAutomationDecisionAssistantAuditShape(t *testing.T) {
	s, al, a := newDecisionAssistantServer(t)
	doAutomation(t, s, http.MethodPost, "/api/automations/"+a.ID+"/set_decision_assistant",
		map[string]any{"backend": "pi-prompt", "model": "qwen3.8:27b", "hooks": []string{"empty_edge"}}, http.StatusOK)
	entries, err := al.Read(audit.QueryFilter{Action: "automation_set_decision_assistant"})
	if err != nil || len(entries) != 1 {
		t.Fatalf("audit read: %v entries=%d", err, len(entries))
	}
	e := entries[0]
	if e.Details["resource_type"] != "automation" || e.Details["resource_id"] != a.ID {
		t.Errorf("resource = %v/%v, want automation/%s", e.Details["resource_type"], e.Details["resource_id"], a.ID)
	}
	if hooks, _ := e.Details["hooks"].([]any); len(hooks) != 1 || hooks[0] != "empty_edge" {
		t.Errorf("hooks = %v", e.Details["hooks"])
	}
}
