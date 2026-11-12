// Per-plugin token usage listing (GET /api/plugins/{name}/tokens).
// Operator-authenticated only; returns secret names and the manifest
// fields that carry their references, plus the names and creation times
// of the service's tokens — never a secret's value and never a token's
// value.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/plugins"
	"github.com/dmz006/datawatch/internal/secrets"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

const pluginTokenAdmin = "ops-admin-token"

// fakeManifestSource serves manifests by plugin name.
type fakeManifestSource map[string]plugins.Manifest

func (f fakeManifestSource) Manifest(name string) (plugins.Manifest, bool) {
	m, ok := f[name]
	return m, ok
}

// fakeSecretStore implements secrets.Store. Get returns a sentinel value
// so any secret value leaking into a response body is easy to spot.
type fakeSecretStore struct{ names map[string]bool }

func (f fakeSecretStore) List() ([]secrets.Secret, error) { return nil, nil }
func (f fakeSecretStore) Get(name string) (secrets.Secret, error) {
	if !f.names[name] {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	return secrets.Secret{Name: name, Value: "SENTINEL-SECRET-VALUE-" + name}, nil
}
func (f fakeSecretStore) Set(string, string, []string, string, []string) error {
	return nil
}
func (f fakeSecretStore) Delete(string) error              { return nil }
func (f fakeSecretStore) Exists(name string) (bool, error) { return f.names[name], nil }

type pluginTokenEnv struct {
	s      *Server
	h      http.Handler
	tokens map[string]string // service -> minted token value
}

func newPluginTokenEnv(t *testing.T) *pluginTokenEnv {
	t.Helper()
	s := bl90Server(t)
	s.token = pluginTokenAdmin
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"svc": true, "plain": true}})
	s.SetPluginTokens(fakeManifestSource{
		"svc": {
			Name: "svc",
			Kind: plugins.KindService,
			Connection: &plugins.PluginConnection{
				BaseURL: "http://127.0.0.1:8123",
				Auth:    &plugins.PluginAuth{Type: "bearer", Token: "${secret:svc_token}"},
			},
			Env: map[string]string{
				"API_KEY": "${secret:api_key}",
				"PLAIN":   "literal",
			},
		},
		"plain": {Name: "plain"},
	})
	s.secretsStore = fakeSecretStore{names: map[string]bool{"svc_token": true}}
	st, err := secrets.NewServiceTokenStore(t.TempDir() + "/service_tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, svc := range []string{"svc", "other"} {
		v, err := st.Mint(svc, "test")
		if err != nil {
			t.Fatal(err)
		}
		tokens[svc] = v
	}
	s.SetServiceTokenStore(st)
	return &pluginTokenEnv{
		s:      s,
		tokens: tokens,
		h:      s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins)),
	}
}

func pluginTokenDo(t *testing.T, h http.Handler, method, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// assertNoValues fails if the body carries a secret value, a minted token
// value, or a "value" field at all.
func assertNoValues(t *testing.T, e *pluginTokenEnv, body string) {
	t.Helper()
	if strings.Contains(body, "SENTINEL-SECRET-VALUE-") {
		t.Errorf("response body contains a secret value: %q", body)
	}
	for svc, tok := range e.tokens {
		if tok != "" && strings.Contains(body, tok) {
			t.Errorf("response body contains the minted token value for service %q", svc)
		}
	}
	if strings.Contains(body, "\"value\"") {
		t.Errorf("response body carries a value field: %q", body)
	}
}

func TestPluginToken_Listing(t *testing.T) {
	e := newPluginTokenEnv(t)
	rr := pluginTokenDo(t, e.h, http.MethodGet, "/api/plugins/svc/tokens", pluginTokenAdmin)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	var resp struct {
		Plugin  string `json:"plugin"`
		Secrets []struct {
			Name   string   `json:"name"`
			Fields []string `json:"fields"`
			Exists bool     `json:"exists"`
		} `json:"secrets"`
		ServiceTokens []struct {
			Name      string `json:"name"`
			CreatedAt string `json:"created_at"`
		} `json:"service_tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plugin != "svc" {
		t.Errorf("plugin = %q, want svc", resp.Plugin)
	}
	if len(resp.Secrets) != 2 {
		t.Fatalf("secrets = %+v, want 2 entries", resp.Secrets)
	}
	if resp.Secrets[0].Name != "api_key" ||
		!reflect.DeepEqual(resp.Secrets[0].Fields, []string{"env.API_KEY"}) ||
		resp.Secrets[0].Exists {
		t.Errorf("api_key entry = %+v, want fields [env.API_KEY] exists=false", resp.Secrets[0])
	}
	if resp.Secrets[1].Name != "svc_token" ||
		!reflect.DeepEqual(resp.Secrets[1].Fields, []string{"connection.auth.token"}) ||
		!resp.Secrets[1].Exists {
		t.Errorf("svc_token entry = %+v, want fields [connection.auth.token] exists=true", resp.Secrets[1])
	}
	if len(resp.ServiceTokens) != 1 || resp.ServiceTokens[0].Name != "svc" {
		t.Fatalf("service_tokens = %+v, want exactly the token named svc", resp.ServiceTokens)
	}
	if _, err := time.Parse(time.RFC3339, resp.ServiceTokens[0].CreatedAt); err != nil {
		t.Errorf("created_at %q is not RFC3339: %v", resp.ServiceTokens[0].CreatedAt, err)
	}
	assertNoValues(t, e, rr.Body.String())
}

func TestPluginToken_NoSecretsOrTokens(t *testing.T) {
	e := newPluginTokenEnv(t)
	rr := pluginTokenDo(t, e.h, http.MethodGet, "/api/plugins/plain/tokens", pluginTokenAdmin)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	var resp struct {
		Secrets       []any `json:"secrets"`
		ServiceTokens []any `json:"service_tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Secrets) != 0 || len(resp.ServiceTokens) != 0 {
		t.Fatalf("secrets=%v service_tokens=%v, want both empty", resp.Secrets, resp.ServiceTokens)
	}
	assertNoValues(t, e, rr.Body.String())
}

func TestPluginToken_OperatorRequired(t *testing.T) {
	e := newPluginTokenEnv(t)
	rr := pluginTokenDo(t, e.h, http.MethodGet, "/api/plugins/svc/tokens", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %q", rr.Code, rr.Body)
	}
}

func TestPluginToken_FederationPeerRefused(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	if err := store.Add(&multiserver.Entry{
		Name: "peer-ptok", Token: "ptok-full", URL: "http://peer:8080",
		Enabled: true, Federated: true, Capabilities: []string{"full-control"},
	}); err != nil {
		t.Fatal(err)
	}
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"svc": true}})
	s.SetPluginTokens(fakeManifestSource{"svc": {Name: "svc"}})
	h := s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins))
	rr := pluginTokenDo(t, h, http.MethodGet, "/api/plugins/svc/tokens", "ptok-full")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %q", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "operator token required") {
		t.Errorf("body %q does not name the refusal", rr.Body)
	}
}

func TestPluginToken_SessionScopedRefused(t *testing.T) {
	e := newPluginTokenEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/svc/tokens", nil)
	ctx := context.WithValue(req.Context(), sessionCapsKey, []string{"config:read", "plugin:read"})
	rr := httptest.NewRecorder()
	e.s.handlePlugins(rr, req.WithContext(ctx))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body %q", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "operator token required") {
		t.Errorf("body %q does not name the refusal", rr.Body)
	}
}

func TestPluginToken_UnknownPlugin(t *testing.T) {
	e := newPluginTokenEnv(t)
	rr := pluginTokenDo(t, e.h, http.MethodGet, "/api/plugins/ghost/tokens", pluginTokenAdmin)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %q", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `plugin "ghost" not found`) {
		t.Errorf("body %q does not name the plugin", rr.Body)
	}
}

func TestPluginToken_MethodNotAllowed(t *testing.T) {
	e := newPluginTokenEnv(t)
	rr := pluginTokenDo(t, e.h, http.MethodPost, "/api/plugins/svc/tokens", pluginTokenAdmin)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405, body %q", rr.Code, rr.Body)
	}
}

func TestPluginToken_Unwired(t *testing.T) {
	s := bl90Server(t)
	s.token = pluginTokenAdmin
	s.SetPluginsAPI(&fakePluginsAPI{known: map[string]bool{"svc": true}})
	// No SetPluginTokens: the route reports the wiring gap.
	rr := pluginTokenDo(t, s.fedAuthMiddleware(http.HandlerFunc(s.handlePlugins)),
		http.MethodGet, "/api/plugins/svc/tokens", pluginTokenAdmin)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body %q", rr.Code, rr.Body)
	}
}

func TestPluginToken_NilSecretsStore(t *testing.T) {
	e := newPluginTokenEnv(t)
	e.s.secretsStore = nil
	rr := pluginTokenDo(t, e.h, http.MethodGet, "/api/plugins/svc/tokens", pluginTokenAdmin)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rr.Code, rr.Body)
	}
	var resp struct {
		Secrets []struct {
			Name   string `json:"name"`
			Exists bool   `json:"exists"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, s := range resp.Secrets {
		if s.Exists {
			t.Errorf("secret %q reports exists=true with no secrets store", s.Name)
		}
	}
}
