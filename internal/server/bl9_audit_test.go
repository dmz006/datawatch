// BL9 — audit REST tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

func TestBL9_Audit_NotEnabled(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rr := httptest.NewRecorder()
	s.handleAudit(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when log nil, got %d", rr.Code)
	}
}

func TestBL9_Audit_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	l, _ := audit.New(t.TempDir())
	defer l.Close()
	s.SetAuditLog(l)
	req := httptest.NewRequest(http.MethodPost, "/api/audit", nil)
	rr := httptest.NewRecorder()
	s.handleAudit(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL9_Audit_ReturnsEntries(t *testing.T) {
	s := bl90Server(t)
	l, _ := audit.New(t.TempDir())
	defer l.Close()
	_ = l.Write(audit.Entry{Actor: "operator", Action: "start", SessionID: "aa"})
	_ = l.Write(audit.Entry{Actor: "channel:signal", Action: "send_input", SessionID: "aa"})
	s.SetAuditLog(l)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rr := httptest.NewRecorder()
	s.handleAudit(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Count   int           `json:"count"`
		Entries []audit.Entry `json:"entries"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Count != 2 {
		t.Errorf("count=%d want 2", got.Count)
	}
}

func TestBL9_Audit_ActorFilter(t *testing.T) {
	s := bl90Server(t)
	l, _ := audit.New(t.TempDir())
	defer l.Close()
	_ = l.Write(audit.Entry{Actor: "operator", Action: "start"})
	_ = l.Write(audit.Entry{Actor: "channel:signal", Action: "send_input"})
	s.SetAuditLog(l)

	req := httptest.NewRequest(http.MethodGet, "/api/audit?actor=operator", nil)
	rr := httptest.NewRecorder()
	s.handleAudit(rr, req)
	var got struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Count != 1 {
		t.Errorf("filter want 1, got %d", got.Count)
	}
}
