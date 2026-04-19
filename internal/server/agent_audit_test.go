// BL107 — REST handler tests for /api/agents/audit.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/agents"
)

func TestHandleAgentAudit_NoPath_503(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/agents/audit", nil)
	rr := httptest.NewRecorder()
	s.handleAgentAudit(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d want 503", rr.Code)
	}
}

func TestHandleAgentAudit_CEF_NotImplemented(t *testing.T) {
	s := &Server{agentAuditPath: "/tmp/x.cef", agentAuditCEF: true}
	req := httptest.NewRequest(http.MethodGet, "/api/agents/audit", nil)
	rr := httptest.NewRecorder()
	s.handleAgentAudit(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("status=%d want 501", rr.Code)
	}
}

func TestHandleAgentAudit_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.jsonl")
	a, err := agents.NewFileAuditor(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Append(agents.AuditEvent{Event: "spawn", AgentID: "x1", Project: "p"})
	a.Append(agents.AuditEvent{Event: "terminate", AgentID: "x1", Project: "p"})
	_ = a.Close()

	s := &Server{agentAuditPath: path}
	req := httptest.NewRequest(http.MethodGet, "/api/agents/audit?event=spawn", nil)
	rr := httptest.NewRecorder()
	s.handleAgentAudit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Path   string             `json:"path"`
		Events []agents.AuditEvent `json:"events"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if body.Path != path {
		t.Errorf("path=%q want %q", body.Path, path)
	}
	if len(body.Events) != 1 || body.Events[0].Event != "spawn" {
		t.Errorf("events=%+v want one spawn", body.Events)
	}
}

func TestHandleAgentAudit_RejectsPost(t *testing.T) {
	s := &Server{agentAuditPath: "/tmp/x"}
	req := httptest.NewRequest(http.MethodPost, "/api/agents/audit", nil)
	rr := httptest.NewRecorder()
	s.handleAgentAudit(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}
