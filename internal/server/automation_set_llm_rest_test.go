// REST tests for POST /api/automations/{id}/set_llm and /set_verification_llm,
// driven through the real autonomous manager.

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
	"github.com/dmz006/datawatch/internal/inference"
)

// automationLLMRecord is the LLM subset of the Automation wire record.
type automationLLMRecord struct {
	ID                  string `json:"id"`
	Version             int64  `json:"version"`
	Backend             string `json:"backend"`
	Model               string `json:"model"`
	Effort              string `json:"effort"`
	PlanningBackend     string `json:"planning_backend"`
	PlanningModel       string `json:"planning_model"`
	VerificationBackend string `json:"verification_backend"`
	VerificationModel   string `json:"verification_model"`
	VerificationEffort  string `json:"verification_effort"`
}

func decodeAutomationLLM(t *testing.T, body string) automationLLMRecord {
	t.Helper()
	var a automationLLMRecord
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("decode automation: %v (body=%q)", err, body)
	}
	return a
}

// newAutomationLLMServer returns a server over a draft Automation and its record, with an
// inference registry that knows the backends "claude-code" and "pi-prompt".
func newAutomationLLMServer(t *testing.T) (*Server, *audit.Log, automationLLMRecord) {
	t.Helper()
	s, mgr, al := newAutomationAuditServer(t)
	reg := inference.NewRegistry()
	for _, n := range []string{"claude-code", "pi-prompt"} {
		if err := reg.Add(&inference.LLM{Name: n, Kind: inference.KindOllama}); err != nil {
			t.Fatalf("registry add: %v", err)
		}
	}
	s.inferenceReg = reg
	id := createDraftAutomation(t, s, mgr)
	a := decodeAutomationLLM(t, doAutomation(t, s, http.MethodGet, "/api/automations/"+id, nil, http.StatusOK))
	return s, al, a
}

func countAutomationAudit(t *testing.T, al *audit.Log, action string) int {
	t.Helper()
	entries, err := al.Read(audit.QueryFilter{Action: action})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	return len(entries)
}

func TestAutomationRESTSetLLM(t *testing.T) {
	s, al, a := newAutomationLLMServer(t)
	path := "/api/automations/" + a.ID + "/set_llm"

	got := decodeAutomationLLM(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"expected_version": a.Version, "actor": "alice", "reason": "tune",
		"backend": "claude-code", "model": "claude-haiku-5-5", "effort": "high",
		"planning_backend": "pi-prompt", "planning_model": "qwen3.8:27b",
	}, http.StatusOK))
	if got.Backend != "claude-code" || got.Model != "claude-haiku-5-5" || got.Effort != "high" ||
		got.PlanningBackend != "pi-prompt" || got.PlanningModel != "qwen3.8:27b" {
		t.Fatalf("set_llm result = %+v", got)
	}
	if got.Version != a.Version+1 {
		t.Errorf("version = %d, want %d", got.Version, a.Version+1)
	}
	if got.VerificationBackend != "" || got.VerificationModel != "" || got.VerificationEffort != "" {
		t.Errorf("verification fields touched: %+v", got)
	}
	wantAudit(t, al, "automation_set_llm", a.ID, map[string]any{
		"actor": "alice", "reason": "tune", "expected_version": float64(a.Version),
		"backend": "claude-code", "model": "claude-haiku-5-5", "effort": "high",
		"planning_backend": "pi-prompt", "planning_model": "qwen3.8:27b",
	})

	// Presence: an omitted field is left alone and "" clears; no expected_version skips the check.
	got = decodeAutomationLLM(t, doAutomation(t, s, http.MethodPost, path, map[string]any{"model": "", "planning_model": ""}, http.StatusOK))
	if got.Model != "" || got.PlanningModel != "" || got.Backend != "claude-code" || got.Effort != "high" || got.PlanningBackend != "pi-prompt" {
		t.Errorf("presence handling: %+v", got)
	}
	wantAudit(t, al, "automation_set_llm", a.ID, map[string]any{"actor": "operator", "expected_version": float64(0), "model": ""})
	if n := countAutomationAudit(t, al, "automation_set_llm"); n != 2 {
		t.Errorf("audit entries = %d, want 2", n)
	}
}

func TestAutomationRESTSetVerificationLLM(t *testing.T) {
	s, al, a := newAutomationLLMServer(t)
	path := "/api/automations/" + a.ID + "/set_verification_llm"

	got := decodeAutomationLLM(t, doAutomation(t, s, http.MethodPost, path, map[string]any{
		"expected_version": a.Version, "actor": "alice", "reason": "verify",
		"backend": "claude-code", "model": "claude-haiku-5-5", "effort": "low",
	}, http.StatusOK))
	if got.VerificationBackend != "claude-code" || got.VerificationModel != "claude-haiku-5-5" || got.VerificationEffort != "low" {
		t.Fatalf("set_verification_llm result = %+v", got)
	}
	if got.Backend != "" || got.Model != "" || got.Effort != "" || got.PlanningBackend != "" {
		t.Errorf("execution fields touched: %+v", got)
	}
	wantAudit(t, al, "automation_set_verification_llm", a.ID, map[string]any{
		"actor": "alice", "reason": "verify", "expected_version": float64(a.Version),
		"backend": "claude-code", "model": "claude-haiku-5-5", "effort": "low",
	})

	got = decodeAutomationLLM(t, doAutomation(t, s, http.MethodPost, path, map[string]any{"effort": ""}, http.StatusOK))
	if got.VerificationEffort != "" || got.VerificationBackend != "claude-code" || got.VerificationModel != "claude-haiku-5-5" {
		t.Errorf("presence handling: %+v", got)
	}
	if n := countAutomationAudit(t, al, "automation_set_verification_llm"); n != 2 {
		t.Errorf("audit entries = %d, want 2", n)
	}
}

func TestAutomationRESTSetLLMGuards(t *testing.T) {
	s, al, a := newAutomationLLMServer(t)
	setLLM := "/api/automations/" + a.ID + "/set_llm"
	setVer := "/api/automations/" + a.ID + "/set_verification_llm"

	t.Run("no setting field", func(t *testing.T) {
		out := doAutomation(t, s, http.MethodPost, setLLM, map[string]any{"expected_version": a.Version, "actor": "x"}, http.StatusBadRequest)
		if !strings.Contains(out, "planning_model") {
			t.Errorf("message should name the allowed fields: %q", out)
		}
		out = doAutomation(t, s, http.MethodPost, setVer, map[string]any{}, http.StatusBadRequest)
		if !strings.Contains(out, "backend, model, effort") || strings.Contains(out, "planning") {
			t.Errorf("verification message: %q", out)
		}
	})
	t.Run("planning on the verification route", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, setVer, map[string]any{"backend": "claude-code", "planning_backend": "pi-prompt"}, http.StatusBadRequest)
		doAutomation(t, s, http.MethodPost, setVer, map[string]any{"planning_model": "m"}, http.StatusBadRequest)
	})
	t.Run("unknown backend", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, setLLM, map[string]any{"backend": "no-such-llm"}, http.StatusBadRequest)
		doAutomation(t, s, http.MethodPost, setLLM, map[string]any{"planning_backend": "no-such-llm"}, http.StatusBadRequest)
		doAutomation(t, s, http.MethodPost, setVer, map[string]any{"backend": "no-such-llm"}, http.StatusBadRequest)
		// An empty backend clears and is not looked up.
		doAutomation(t, s, http.MethodPost, setLLM, map[string]any{"backend": ""}, http.StatusOK)
	})
	t.Run("bad JSON", func(t *testing.T) {
		for _, p := range []string{setLLM, setVer} {
			rr := httptest.NewRecorder()
			s.handleAutomations(rr, httptest.NewRequest(http.MethodPost, p, bytes.NewBufferString("{not json")))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s: code = %d, want 400", p, rr.Code)
			}
		}
	})
	t.Run("stale version", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, setLLM, map[string]any{"expected_version": a.Version + 50, "model": "m"}, http.StatusConflict)
		doAutomation(t, s, http.MethodPost, setVer, map[string]any{"expected_version": a.Version + 50, "model": "m"}, http.StatusConflict)
	})
	t.Run("unknown automation", func(t *testing.T) {
		doAutomation(t, s, http.MethodPost, "/api/automations/nope/set_llm", map[string]any{"model": "m"}, http.StatusNotFound)
		doAutomation(t, s, http.MethodPost, "/api/automations/nope/set_verification_llm", map[string]any{"model": "m"}, http.StatusNotFound)
	})
	t.Run("GET is 405", func(t *testing.T) {
		doAutomation(t, s, http.MethodGet, setLLM, nil, http.StatusMethodNotAllowed)
		doAutomation(t, s, http.MethodGet, setVer, nil, http.StatusMethodNotAllowed)
	})
	t.Run("missing write capability", func(t *testing.T) {
		for _, p := range []string{setLLM, setVer} {
			req := httptest.NewRequest(http.MethodPost, p, bytes.NewBufferString(`{"model":"m"}`))
			req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{"autonomous:read"}))
			rr := httptest.NewRecorder()
			s.handleAutomations(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Errorf("%s: code = %d, want 403", p, rr.Code)
			}
		}
	})

	// Only the one successful call above (the empty backend) wrote an audit entry.
	if n := countAutomationAudit(t, al, "automation_set_llm"); n != 1 {
		t.Errorf("automation_set_llm audit entries = %d, want 1", n)
	}
	if n := countAutomationAudit(t, al, "automation_set_verification_llm"); n != 0 {
		t.Errorf("automation_set_verification_llm audit entries = %d, want 0", n)
	}
}
