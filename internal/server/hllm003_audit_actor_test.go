// HLLM-003 — audit entries hardcoded Actor:"operator" regardless of who
// actually presented the credential, so a spawned session's secret_get (or
// config write) read identically to the operator's own in the audit log.
// These tests pin auditActor's derivation and its wiring into the two
// concrete examples the design doc names: secret_access and config writes.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/secrets"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

func TestAuditActor_AdminContextIsOperator(t *testing.T) {
	s := &Server{}
	if got := s.auditActor(context.Background()); got != "operator" {
		t.Fatalf("got %q, want operator", got)
	}
}

func TestAuditActor_SessionTokenNamesOwningSession(t *testing.T) {
	store, err := auth.NewSessionTokenStore(filepath.Join(t.TempDir(), "session-tokens.json"))
	if err != nil {
		t.Fatalf("NewSessionTokenStore: %v", err)
	}
	tok, err := store.Mint("testhost-sess-1", []string{"secrets:read"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	s := &Server{sessionTokens: store}

	ctx := context.WithValue(context.Background(), sessionCapsKey, []string{"secrets:read"})
	ctx = context.WithValue(ctx, callerTokenKey, tok)

	got := s.auditActor(ctx)
	if got != "session:testhost-sess-1" {
		t.Fatalf("got %q, want session:testhost-sess-1", got)
	}
}

func TestAuditActor_FederationPeerNamesItself(t *testing.T) {
	s := &Server{}
	ctx := context.WithValue(context.Background(), fedPeerKey, &multiserver.Entry{Name: "peer1"})
	if got := s.auditActor(ctx); got != "peer:peer1" {
		t.Fatalf("got %q, want peer:peer1", got)
	}
}

// TestHandleSecretsGet_AuditsRealCallerNotHardcodedOperator is the exact
// scenario the design doc names as HLLM-003's acceptance test: a session's
// secret_get must be audited as session:<id>, not operator.
func TestHandleSecretsGet_AuditsRealCallerNotHardcodedOperator(t *testing.T) {
	dataDir := t.TempDir()
	store, err := secrets.NewBuiltinStore(dataDir)
	if err != nil {
		t.Fatalf("secrets.NewBuiltinStore: %v", err)
	}
	if err := store.Set("hllm-probe", "super-secret", nil, "", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	al, err := audit.New(dataDir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })

	tokStore, err := auth.NewSessionTokenStore(filepath.Join(dataDir, "session-tokens.json"))
	if err != nil {
		t.Fatalf("NewSessionTokenStore: %v", err)
	}
	tok, err := tokStore.Mint("testhost-sec-sandbox-18a6", []string{federation.CapSecretsRead})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	s := &Server{secretsStore: store, auditLog: al, sessionTokens: tokStore}

	ctx := context.WithValue(context.Background(), sessionCapsKey, []string{federation.CapSecretsRead})
	ctx = context.WithValue(ctx, callerTokenKey, tok)
	req := httptest.NewRequest(http.MethodGet, "/api/secrets/hllm-probe", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	s.handleSecretsGet(rr, req, "hllm-probe")

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	entries, err := al.Read(audit.QueryFilter{Action: "secret_access"})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 secret_access entry, got %d", len(entries))
	}
	if entries[0].Actor != "session:testhost-sec-sandbox-18a6" {
		t.Fatalf("secret_access audited as %q, want session:testhost-sec-sandbox-18a6 (not operator)", entries[0].Actor)
	}
}

func TestAuditConfigPatch_NamesSessionActor(t *testing.T) {
	dataDir := t.TempDir()
	al, err := audit.New(dataDir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })

	tokStore, err := auth.NewSessionTokenStore(filepath.Join(dataDir, "session-tokens.json"))
	if err != nil {
		t.Fatalf("NewSessionTokenStore: %v", err)
	}
	tok, err := tokStore.Mint("testhost-config-writer", []string{federation.CapConfigWrite})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	s := newTestServer(t, nil, nil)
	s.cfg = config.DefaultConfig()
	s.cfgPath = filepath.Join(dataDir, "config.yaml")
	s.auditLog = al
	s.sessionTokens = tokStore

	ctx := context.WithValue(context.Background(), sessionCapsKey, []string{federation.CapConfigWrite})
	ctx = context.WithValue(ctx, callerTokenKey, tok)
	body, _ := json.Marshal(map[string]string{"server.host": "0.0.0.0"})
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)).WithContext(ctx)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	entries, err := al.Read(audit.QueryFilter{Action: "configure"})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 configure entry, got %d", len(entries))
	}
	if entries[0].Actor != "session:testhost-config-writer" {
		t.Fatalf("configure audited as %q, want session:testhost-config-writer (not operator)", entries[0].Actor)
	}
}
