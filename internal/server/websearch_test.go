// BL391 — tests for the /api/websearch/* provider CRUD surface, with a
// focus on the secret-ref masking in saveWebSearchConfig: a provider whose
// api_key was configured as a "${secret:name}" ref and resolved to plaintext
// in memory by secrets.ResolveConfig at daemon startup must never have that
// plaintext persisted to config.yaml by an edit that doesn't itself touch
// api_key (e.g. a plain enable/disable toggle).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func websearchTestServer(t *testing.T) *Server {
	t.Helper()
	s := bl90Server(t)
	s.cfg.WebSearch.Providers = []config.SearchProvider{
		{Name: "brave", Type: "brave", Enabled: true, Priority: 1, APIKey: "resolved-plaintext-key", NumResults: 10},
		{Name: "searx", Type: "searxng", Enabled: false, Priority: 2, URL: "http://localhost:8888", Engine: "bing", NumResults: 10},
	}
	// Mirrors main.go's startup snapshot: "brave" was configured with a
	// ${secret:...} ref, now resolved in cfg to plaintext; "searx" never
	// had a ref, so no masking should ever apply to it.
	s.websearchAPIKeyRefs = map[string]string{
		"brave": "${secret:brave_search_api_key}",
		"searx": "",
	}
	return s
}

func readWebSearchConfigFile(t *testing.T, s *Server) string {
	t.Helper()
	b, err := os.ReadFile(s.cfgPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	return string(b)
}

// TestWebSearchEnable_DoesNotPersistResolvedSecret is the core regression
// test for the vulnerability this file's saveWebSearchConfig fixes: toggling
// a provider's enabled state must save the ${secret:...} ref to disk, never
// the resolved plaintext key sitting in memory.
func TestWebSearchEnable_DoesNotPersistResolvedSecret(t *testing.T) {
	s := websearchTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/websearch/providers/brave/disable", nil)
	rr := httptest.NewRecorder()
	s.handleWebSearchProviders(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("disable: status %d body=%s", rr.Code, rr.Body.String())
	}

	saved := readWebSearchConfigFile(t, s)
	if strings.Contains(saved, "resolved-plaintext-key") {
		t.Fatalf("plaintext secret leaked to config.yaml:\n%s", saved)
	}
	if !strings.Contains(saved, "${secret:brave_search_api_key}") {
		t.Fatalf("expected secret ref restored in saved config, got:\n%s", saved)
	}

	// In-memory state must stay resolved (so Search() keeps working without
	// a restart) and must reflect the toggle that was just requested.
	idx := findProvider(s.cfg.WebSearch.Providers, "brave")
	if s.cfg.WebSearch.Providers[idx].APIKey != "resolved-plaintext-key" {
		t.Errorf("in-memory api_key was mutated: %q", s.cfg.WebSearch.Providers[idx].APIKey)
	}
	if s.cfg.WebSearch.Providers[idx].Enabled {
		t.Errorf("provider should be disabled after the request")
	}
}

// TestWebSearchUpdate_ExplicitAPIKeySavedAsGiven verifies the operator's own
// explicit api_key edit is never overwritten by the masking logic.
func TestWebSearchUpdate_ExplicitAPIKeySavedAsGiven(t *testing.T) {
	s := websearchTestServer(t)
	body := `{"api_key":"${secret:new_brave_key}"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/websearch/providers/brave", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleWebSearchProviders(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update: status %d body=%s", rr.Code, rr.Body.String())
	}

	saved := readWebSearchConfigFile(t, s)
	if !strings.Contains(saved, "${secret:new_brave_key}") {
		t.Fatalf("expected the operator-supplied ref to be saved as given, got:\n%s", saved)
	}
	if strings.Contains(saved, "${secret:brave_search_api_key}") {
		t.Fatalf("stale startup ref should not appear after an explicit api_key edit:\n%s", saved)
	}
}

// TestWebSearchUpdate_OtherFieldDoesNotLeakSecret verifies a PATCH that
// touches an unrelated field (priority) still masks the secret on save.
func TestWebSearchUpdate_OtherFieldDoesNotLeakSecret(t *testing.T) {
	s := websearchTestServer(t)
	body := `{"priority":5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/websearch/providers/brave", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleWebSearchProviders(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update: status %d body=%s", rr.Code, rr.Body.String())
	}

	saved := readWebSearchConfigFile(t, s)
	if strings.Contains(saved, "resolved-plaintext-key") {
		t.Fatalf("plaintext secret leaked to config.yaml on an unrelated field edit:\n%s", saved)
	}
}

// TestWebSearchProvidersList_ReturnsConfiguredProviders exercises the plain
// GET list path.
func TestWebSearchProvidersList_ReturnsConfiguredProviders(t *testing.T) {
	s := websearchTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/websearch/providers", nil)
	rr := httptest.NewRecorder()
	s.handleWebSearchProviders(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: status %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Providers []providerView `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(out.Providers))
	}
}

// TestWebSearchProvidersList_NeverEchoesResolvedAPIKey is the GET-path
// counterpart of the save-masking tests above: the resolved plaintext
// secret held in memory for Registry.Search must never appear in a REST
// (or, by the same code path, CLI/MCP/comm-channel) read response.
func TestWebSearchProvidersList_NeverEchoesResolvedAPIKey(t *testing.T) {
	s := websearchTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/websearch/providers", nil)
	rr := httptest.NewRecorder()
	s.handleWebSearchProviders(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: status %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "resolved-plaintext-key") {
		t.Fatalf("resolved plaintext secret leaked in GET response:\n%s", rr.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/websearch/providers/brave", nil)
	rr2 := httptest.NewRecorder()
	s.handleWebSearchProviders(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("get: status %d body=%s", rr2.Code, rr2.Body.String())
	}
	if strings.Contains(rr2.Body.String(), "resolved-plaintext-key") {
		t.Fatalf("resolved plaintext secret leaked in GET-single response:\n%s", rr2.Body.String())
	}
}

// TestWebSearchDelete_RemovesProviderWithoutLeakingOthersSecret verifies
// deleting one provider still masks the remaining provider's secret.
func TestWebSearchDelete_RemovesProviderWithoutLeakingOthersSecret(t *testing.T) {
	s := websearchTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/websearch/providers/searx", nil)
	rr := httptest.NewRecorder()
	s.handleWebSearchProviders(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d body=%s", rr.Code, rr.Body.String())
	}
	if findProvider(s.cfg.WebSearch.Providers, "searx") >= 0 {
		t.Fatalf("searx provider should have been removed")
	}
	saved := readWebSearchConfigFile(t, s)
	if strings.Contains(saved, "resolved-plaintext-key") {
		t.Fatalf("plaintext secret leaked to config.yaml during unrelated delete:\n%s", saved)
	}
}
