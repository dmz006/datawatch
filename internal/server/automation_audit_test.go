// AGENT.md's Audit Logging Rule: the /api/automations handler emits the
// three automation lifecycle actions (automation_create,
// automation_update, automation_delete) into the operator audit log with
// resource_type "automation", exercising the real autonomous manager
// end-to-end.

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// newAutomationAuditServer wires a real autonomous manager (through
// autonomous.API, the same adapter main.go uses) plus a file-backed audit
// log, and returns all three.
func newAutomationAuditServer(t *testing.T) (*Server, *autonomous.Manager, *audit.Log) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	return &Server{cfg: config.DefaultConfig(), autonomousMgr: autonomous.NewAPI(mgr), auditLog: al}, mgr, al
}

// doAutomation runs one /api/automations request and fails unless it
// returns wantStatus; the response body (if any) is returned raw.
func doAutomation(t *testing.T, s *Server, method, path string, body any, wantStatus int) string {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	rr := httptest.NewRecorder()
	s.handleAutomations(rr, req)
	if rr.Code != wantStatus {
		t.Fatalf("%s %s: want %d, got %d body=%s", method, path, wantStatus, rr.Code, rr.Body.String())
	}
	return rr.Body.String()
}

func TestAutomationAuditEmitsCreateUpdateDelete(t *testing.T) {
	s, mgr, al := newAutomationAuditServer(t)
	// The manager rejects members pointing at missing PRDs, so one
	// real member PRD is needed before an Automation can exist.
	prd, err := mgr.CreatePRD("cve sweep spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	members := []any{map[string]any{"prd_id": prd.ID}}

	created := doAutomation(t, s, http.MethodPost, "/api/automations", map[string]any{
		"name": "cve sweep", "actor": "operator", "reason": "nightly sweep", "members": members,
	}, http.StatusCreated)
	var a struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal([]byte(created), &a); err != nil || a.ID == "" {
		t.Fatalf("decode create response: %v (body=%q)", err, created)
	}

	doAutomation(t, s, http.MethodPut, "/api/automations/"+a.ID, map[string]any{
		"name": "cve sweep v2", "actor": "operator", "reason": "widen scope",
		"expected_version": a.Version, "members": members,
	}, http.StatusOK)

	doAutomation(t, s, http.MethodDelete, "/api/automations/"+a.ID, map[string]any{
		"actor": "operator", "reason": "no longer needed",
	}, http.StatusNoContent)

	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	automationActions := []string{"automation_create", "automation_update", "automation_delete"}
	byAction := map[string]audit.Entry{}
	for _, e := range entries {
		for _, want := range automationActions {
			if e.Action == want {
				byAction[want] = e
			}
		}
	}
	for _, want := range automationActions {
		e, ok := byAction[want]
		if !ok {
			t.Errorf("no audit entry for %q: %+v", want, entries)
			continue
		}
		if e.Details["resource_type"] != "automation" {
			t.Errorf("%s resource_type = %v, want automation", want, e.Details["resource_type"])
		}
	}
	if e := byAction["automation_create"]; e.Details["name"] != "cve sweep" {
		t.Errorf("automation_create name = %v, want %q", e.Details["name"], "cve sweep")
	}
	if got, _ := byAction["automation_update"].Details["version"].(float64); got != float64(a.Version+1) {
		t.Errorf("automation_update version = %v, want %d", byAction["automation_update"].Details["version"], a.Version+1)
	}
	if e := byAction["automation_delete"]; e.Details["resource_id"] != a.ID {
		t.Errorf("automation_delete resource_id = %v, want %q", e.Details["resource_id"], a.ID)
	}
}
