// SEC-017 — PUT /api/config writes must be audited: which keys changed,
// with credential-shaped values masked rather than logged in the clear.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

func newConfigAuditTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	cfg := config.DefaultConfig()
	s.cfg = cfg
	s.cfgPath = filepath.Join(t.TempDir(), "config.yaml")
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s.auditLog = al
	return s
}

func putConfig(t *testing.T, s *Server, patch map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleConfig)).ServeHTTP(rr, req)
	return rr
}

func TestAuditConfigPatch_RecordsAppliedKeys(t *testing.T) {
	s := newConfigAuditTestServer(t)
	rr := putConfig(t, s, map[string]interface{}{"server.host": "0.0.0.0", "server.port": 9090})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	entries, err := s.auditLog.Read(audit.QueryFilter{Action: "configure"})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 configure entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Actor != "operator" {
		t.Errorf("actor=%q, want operator", e.Actor)
	}
	keys, _ := e.Details["keys"].([]interface{})
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys recorded, got %v", e.Details["keys"])
	}
}

func TestAuditConfigPatch_MasksCredentialShapedValues(t *testing.T) {
	s := newConfigAuditTestServer(t)
	rr := putConfig(t, s, map[string]interface{}{"webhook.token": "super-secret-value-12345"})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	entries, err := s.auditLog.Read(audit.QueryFilter{Action: "configure"})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 configure entry, got %d", len(entries))
	}
	changes, _ := entries[0].Details["changes"].(map[string]interface{})
	got, _ := changes["webhook.token"].(string)
	if got == "super-secret-value-12345" {
		t.Fatal("credential-shaped value must be masked in the audit log, not logged in the clear")
	}
	if got == "" || got == "***" {
		t.Errorf("expected a partially-masked value (first/last 2 chars visible), got %q", got)
	}
	if got[:2] != "su" || got[len(got)-2:] != "45" {
		t.Errorf("expected first 2 / last 2 chars preserved, got %q", got)
	}
}

func TestAuditConfigPatch_SkippedKeysExcludedFromAudit(t *testing.T) {
	s := newConfigAuditTestServer(t)
	s.cfg.Server.Token = "admin-token"
	rr := putConfig(t, s, map[string]interface{}{
		"server.token": "attempted-token-change",
		"server.host":  "0.0.0.0",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	entries, err := s.auditLog.Read(audit.QueryFilter{Action: "configure"})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 configure entry, got %d", len(entries))
	}
	keys, _ := entries[0].Details["keys"].([]interface{})
	for _, k := range keys {
		if k == "server.token" {
			t.Fatal("server.token was skipped by applyConfigPatch and must not appear in the audit entry's keys")
		}
	}
	if len(keys) != 1 {
		t.Fatalf("expected only server.host recorded, got %v", keys)
	}
}

func TestAuditConfigPatch_NoAuditLogConfiguredIsANoop(t *testing.T) {
	s := newConfigAuditTestServer(t)
	s.auditLog = nil // simulate audit logging disabled/unconfigured
	rr := putConfig(t, s, map[string]interface{}{"server.host": "0.0.0.0"})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/config must still succeed with no audit log configured, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestIsSensitiveConfigKey(t *testing.T) {
	cases := map[string]bool{
		"webhook.token":     true,
		"email.password":    true,
		"twilio.auth_token": true,
		"server.host":       false,
		"server.port":       false,
		"detection.enabled":  false,
	}
	for key, want := range cases {
		if got := isSensitiveConfigKey(key); got != want {
			t.Errorf("isSensitiveConfigKey(%q) = %v, want %v", key, got, want)
		}
	}
}
