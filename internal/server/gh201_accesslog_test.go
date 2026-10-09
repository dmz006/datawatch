// GH#201 — HTTP access / WS lifecycle / auth-failure log.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

func TestGH201_FedAuthMiddleware_LogsAccessAndAuthFailure(t *testing.T) {
	s := bl90Server(t)
	s.token = "admin-secret-token"
	accessDir := t.TempDir()
	accessLog, err := audit.NewAt(accessDir + "/access.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accessLog.Close() }()
	s.SetAccessLog(accessLog)

	mw := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Successful admin request.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer admin-secret-token")
	req.Header.Set("User-Agent", "test-agent/1.0")
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Failed request — wrong token.
	badReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	badReq.Header.Set("Authorization", "Bearer wrong-token-entirely")
	badRR := httptest.NewRecorder()
	mw.ServeHTTP(badRR, badReq)
	if badRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", badRR.Code)
	}

	entries, err := accessLog.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 access-log entries, got %d: %+v", len(entries), entries)
	}

	var sawAccess, sawFailure bool
	raw, _ := json.Marshal(entries)
	rawStr := string(raw)
	for _, e := range entries {
		switch e.Action {
		case "http_access":
			sawAccess = true
			if e.Actor != "admin" {
				t.Errorf("http_access actor = %q, want admin", e.Actor)
			}
		case "auth_failure":
			sawFailure = true
			if e.Actor != "unauthenticated" {
				t.Errorf("auth_failure actor = %q, want unauthenticated", e.Actor)
			}
		}
	}
	if !sawAccess || !sawFailure {
		t.Fatalf("expected both http_access and auth_failure entries, got %+v", entries)
	}

	// GH#201's core safety property: never log the token itself.
	if strings.Contains(rawStr, "admin-secret-token") || strings.Contains(rawStr, "wrong-token-entirely") {
		t.Fatalf("access log entries contain a raw token value: %s", rawStr)
	}
}

func TestGH201_HandleAuditAccess_RequiresGet(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/audit/access", nil)
	rr := httptest.NewRecorder()
	s.handleAuditAccess(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST should be 405, got %d", rr.Code)
	}
}

func TestGH201_HandleAuditAccess_DisabledWithoutLog(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/access", nil)
	rr := httptest.NewRecorder()
	s.handleAuditAccess(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when access log unset, got %d", rr.Code)
	}
}

func TestGH201_HandleAuditAccess_ReturnsEntries(t *testing.T) {
	s := bl90Server(t)
	accessLog, err := audit.NewAt(t.TempDir() + "/access.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accessLog.Close() }()
	s.SetAccessLog(accessLog)
	if err := accessLog.Write(audit.Entry{Actor: "admin", Action: "http_access"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/audit/access", nil)
	rr := httptest.NewRecorder()
	s.handleAuditAccess(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Count   int           `json:"count"`
		Entries []audit.Entry `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Count != 1 || len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %+v", resp)
	}
}
