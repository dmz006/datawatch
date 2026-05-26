// Issue #95 — unit tests for GET/PUT /api/opencode/providers.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/secrets"
	"github.com/dmz006/datawatch/internal/session"
)

// ---- minimal in-memory secretsStore for these tests -------------------------

type providerTestStore struct {
	data map[string]secrets.Secret
	setErr error
}

func newProviderTestStore() *providerTestStore {
	return &providerTestStore{data: map[string]secrets.Secret{}}
}

func (s *providerTestStore) List() ([]secrets.Secret, error) {
	out := make([]secrets.Secret, 0, len(s.data))
	for _, v := range s.data {
		out = append(out, v)
	}
	return out, nil
}

func (s *providerTestStore) Get(name string) (secrets.Secret, error) {
	v, ok := s.data[name]
	if !ok {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	return v, nil
}

func (s *providerTestStore) Set(name, value string, tags []string, desc string, scopes []string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.data[name] = secrets.Secret{
		Name:      name,
		Value:     value,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return nil
}

func (s *providerTestStore) Delete(name string) error {
	delete(s.data, name)
	return nil
}

func (s *providerTestStore) Exists(name string) (bool, error) {
	_, ok := s.data[name]
	return ok, nil
}

// ---- helper -----------------------------------------------------------------

func newProviderTestServer(t *testing.T, st secretsStore) *Server {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hub := NewHub()
	srv := NewServer(hub, sm, "h", "", nil, nil, "")
	srv.secretsStore = st
	return srv
}

// ---- tests ------------------------------------------------------------------

// TestHandleOpenCodeProviders_GET_Empty verifies that GET returns all three
// known providers with api_key_set=false when no keys are stored.
func TestHandleOpenCodeProviders_GET_Empty(t *testing.T) {
	srv := newProviderTestServer(t, newProviderTestStore())
	req := httptest.NewRequest(http.MethodGet, "/api/opencode/providers", nil)
	w := httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET status %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Providers map[string]map[string]bool `json:"providers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range []string{"anthropic", "openai", "google"} {
		if resp.Providers[p]["api_key_set"] {
			t.Errorf("provider %s: want api_key_set=false, got true", p)
		}
	}
}

// TestHandleOpenCodeProviders_PUT_GET roundtrip: PUT a key then GET shows
// api_key_set=true for that provider.
func TestHandleOpenCodeProviders_PUT_GET(t *testing.T) {
	store := newProviderTestStore()
	srv := newProviderTestServer(t, store)

	// PUT an Anthropic key.
	body, _ := json.Marshal(map[string]string{"provider": "anthropic", "api_key": "sk-ant-test"})
	req := httptest.NewRequest(http.MethodPut, "/api/opencode/providers", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status %d, body: %s", w.Code, w.Body.String())
	}

	// Verify secret stored under expected name and value is not exposed.
	sec, err := store.Get("opencode_provider_anthropic")
	if err != nil {
		t.Fatalf("secret not stored: %v", err)
	}
	if sec.Value != "sk-ant-test" {
		t.Errorf("stored value mismatch: got %q", sec.Value)
	}

	// GET — anthropic should now be api_key_set=true.
	req = httptest.NewRequest(http.MethodGet, "/api/opencode/providers", nil)
	w = httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status %d", w.Code)
	}
	var resp struct {
		Providers map[string]map[string]bool `json:"providers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Providers["anthropic"]["api_key_set"] {
		t.Error("anthropic: want api_key_set=true after PUT, got false")
	}
	if resp.Providers["openai"]["api_key_set"] {
		t.Error("openai: want api_key_set=false, got true")
	}
}

// TestHandleOpenCodeProviders_PUT_UnknownProvider returns 400 for unknown
// provider names.
func TestHandleOpenCodeProviders_PUT_UnknownProvider(t *testing.T) {
	srv := newProviderTestServer(t, newProviderTestStore())
	body, _ := json.Marshal(map[string]string{"provider": "cohere", "api_key": "key"})
	req := httptest.NewRequest(http.MethodPut, "/api/opencode/providers", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// TestHandleOpenCodeProviders_PUT_EmptyKey returns 400 for empty api_key.
func TestHandleOpenCodeProviders_PUT_EmptyKey(t *testing.T) {
	srv := newProviderTestServer(t, newProviderTestStore())
	body, _ := json.Marshal(map[string]string{"provider": "openai", "api_key": ""})
	req := httptest.NewRequest(http.MethodPut, "/api/opencode/providers", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// TestHandleOpenCodeProviders_NoSecretsStore returns 503 when no store is wired.
func TestHandleOpenCodeProviders_NoSecretsStore(t *testing.T) {
	srv := newProviderTestServer(t, nil)
	body, _ := json.Marshal(map[string]string{"provider": "google", "api_key": "key"})
	req := httptest.NewRequest(http.MethodPut, "/api/opencode/providers", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
}

// TestHandleOpenCodeProviders_MethodNotAllowed returns 405 for PATCH.
func TestHandleOpenCodeProviders_MethodNotAllowed(t *testing.T) {
	srv := newProviderTestServer(t, newProviderTestStore())
	req := httptest.NewRequest(http.MethodPatch, "/api/opencode/providers", nil)
	w := httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// TestHandleOpenCodeProviders_GET_NeverReturnsKeyValue verifies that GET
// does not expose the actual stored key value.
func TestHandleOpenCodeProviders_GET_NeverReturnsKeyValue(t *testing.T) {
	store := newProviderTestStore()
	srv := newProviderTestServer(t, store)

	// Pre-populate a key directly.
	_ = store.Set("opencode_provider_openai", "sk-openai-secret", nil, "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/opencode/providers", nil)
	w := httptest.NewRecorder()
	srv.handleOpenCodeProviders(w, req)

	body := w.Body.String()
	if providerBodyContains(body, "sk-openai-secret") {
		t.Error("GET response must not contain the actual API key value")
	}
}

func providerBodyContains(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
