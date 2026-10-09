// GH#203 — external-service secrets: an independent service (not a
// spawned F10 agent, not a federation peer) resolves a specific,
// per-secret-scoped set of secrets via a persistent, operator-minted
// token.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/secrets"
)

// fakeSecretsStore is a minimal in-memory secretsStore for these tests.
type fakeSecretsStore struct {
	m map[string]secrets.Secret
}

func (f *fakeSecretsStore) List() ([]secrets.Secret, error) {
	out := make([]secrets.Secret, 0, len(f.m))
	for _, s := range f.m {
		out = append(out, s)
	}
	return out, nil
}
func (f *fakeSecretsStore) Get(name string) (secrets.Secret, error) {
	s, ok := f.m[name]
	if !ok {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	return s, nil
}
func (f *fakeSecretsStore) Set(name, value string, tags []string, description string, scopes []string) error {
	f.m[name] = secrets.Secret{Name: name, Value: value, Tags: tags, Description: description, Scopes: scopes}
	return nil
}
func (f *fakeSecretsStore) Delete(name string) error { delete(f.m, name); return nil }
func (f *fakeSecretsStore) Exists(name string) (bool, error) {
	_, ok := f.m[name]
	return ok, nil
}

func TestHandleExternalSecretsGet_ScopedServiceCanRead(t *testing.T) {
	s := bl90Server(t)
	svcStore, err := secrets.NewServiceTokenStore(t.TempDir() + "/service_tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	s.SetServiceTokenStore(svcStore)
	tok, err := svcStore.Mint("imap-mcp", "")
	if err != nil {
		t.Fatal(err)
	}
	s.SetSecretsStore(&fakeSecretsStore{m: map[string]secrets.Secret{
		"imap_mcp_token_datawatch": {Name: "imap_mcp_token_datawatch", Value: "shh", Scopes: []string{"service:imap-mcp"}},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/external/secrets/imap_mcp_token_datawatch", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	s.handleExternalSecretsGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct{ Name, Value string }
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Value != "shh" {
		t.Errorf("value = %q, want shh", resp.Value)
	}
}

func TestHandleExternalSecretsGet_OutOfScopeServiceDenied(t *testing.T) {
	s := bl90Server(t)
	svcStore, err := secrets.NewServiceTokenStore(t.TempDir() + "/service_tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	s.SetServiceTokenStore(svcStore)
	tok, _ := svcStore.Mint("some-other-service", "")
	s.SetSecretsStore(&fakeSecretsStore{m: map[string]secrets.Secret{
		"imap_mcp_token_datawatch": {Name: "imap_mcp_token_datawatch", Value: "shh", Scopes: []string{"service:imap-mcp"}},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/external/secrets/imap_mcp_token_datawatch", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	s.handleExternalSecretsGet(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleExternalSecretsGet_UnknownTokenUnauthorized(t *testing.T) {
	s := bl90Server(t)
	svcStore, err := secrets.NewServiceTokenStore(t.TempDir() + "/service_tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	s.SetServiceTokenStore(svcStore)
	s.SetSecretsStore(&fakeSecretsStore{m: map[string]secrets.Secret{}})

	req := httptest.NewRequest(http.MethodGet, "/api/external/secrets/anything", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rr := httptest.NewRecorder()
	s.handleExternalSecretsGet(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rr.Code)
	}
}

func TestHandleSecretServiceTokens_MintListRevoke(t *testing.T) {
	s := bl90Server(t)
	svcStore, err := secrets.NewServiceTokenStore(t.TempDir() + "/service_tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	s.SetServiceTokenStore(svcStore)
	s.token = "" // no-token-configured branch of fedAuthMiddleware isn't exercised here; call handler directly

	// Mint.
	body, _ := json.Marshal(map[string]string{"name": "imap-mcp", "description": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/secrets/service-tokens", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleSecretServiceTokens(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("mint status %d body=%s", rr.Code, rr.Body.String())
	}
	var mintResp struct{ Name, Token string }
	if err := json.Unmarshal(rr.Body.Bytes(), &mintResp); err != nil {
		t.Fatal(err)
	}
	if mintResp.Token == "" {
		t.Fatal("mint response did not include a token")
	}

	// List — must never include the token value.
	listReq := httptest.NewRequest(http.MethodGet, "/api/secrets/service-tokens", nil)
	listRR := httptest.NewRecorder()
	s.handleSecretServiceTokens(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status %d", listRR.Code)
	}
	if bytes.Contains(listRR.Body.Bytes(), []byte(mintResp.Token)) {
		t.Error("list response must never contain the token value")
	}

	// Revoke.
	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/secrets/service-tokens/imap-mcp", nil)
	revokeRR := httptest.NewRecorder()
	s.handleSecretServiceTokens(revokeRR, revokeReq)
	if revokeRR.Code != http.StatusOK {
		t.Fatalf("revoke status %d body=%s", revokeRR.Code, revokeRR.Body.String())
	}
	if _, ok := svcStore.Lookup(mintResp.Token); ok {
		t.Error("token should no longer resolve after revoke via the REST handler")
	}
}
