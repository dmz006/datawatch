// BL267 (v6.15.0) — VaultStore tests against an httptest mock Vault.
//
// The mock implements the minimum surface VaultStore exercises:
//   POST /v1/<mount>/data/<...>     — Set
//   GET  /v1/<mount>/data/<...>     — Get
//   LIST /v1/<mount>/metadata/<...> — List
//   DELETE /v1/<mount>/metadata/<...> — Delete
//   GET  /v1/sys/health              — CheckHealth
//
// Round-trips operator fields (value, description, tags, scopes) and
// verifies the X-Vault-Request-ID + X-Vault-Token + X-Vault-Namespace
// headers are sent + parsed correctly.

package secrets

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockVault is a single-tenant in-memory Vault stand-in: stores secrets
// in a flat map keyed by the full path.
type mockVault struct {
	t       *testing.T
	store   map[string]map[string]string
	reqIDs  int
	wantTok string
	wantNS  string
}

func newMockVault(t *testing.T, wantToken, wantNamespace string) *mockVault {
	return &mockVault{t: t, store: map[string]map[string]string{}, wantTok: wantToken, wantNS: wantNamespace}
}

func (m *mockVault) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Vault-Token"); got != m.wantTok {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		if m.wantNS != "" && r.Header.Get("X-Vault-Namespace") != m.wantNS {
			http.Error(w, "bad namespace", http.StatusForbidden)
			return
		}
		m.reqIDs++
		reqID := "test-req-" + nextID(m.reqIDs)
		w.Header().Set("X-Vault-Request-ID", reqID)

		switch {
		case r.URL.Path == "/v1/sys/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"initialized":true,"sealed":false,"standby":false}`))
			return
		case r.Method == "LIST":
			prefix := strings.TrimPrefix(r.URL.Path, "/v1/")
			prefix = strings.Replace(prefix, "/metadata/", "/data/", 1) + "/"
			seen := map[string]bool{}
			for k := range m.store {
				if !strings.HasPrefix(k, prefix) {
					continue
				}
				rest := strings.TrimPrefix(k, prefix)
				if idx := strings.Index(rest, "/"); idx >= 0 {
					seen[rest[:idx]+"/"] = true
				} else {
					seen[rest] = true
				}
			}
			keys := make([]string, 0, len(seen))
			for k := range seen {
				keys = append(keys, k)
			}
			out := map[string]interface{}{
				"request_id": reqID,
				"data":       map[string]interface{}{"keys": keys},
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/data/"):
			path := strings.TrimPrefix(r.URL.Path, "/v1/")
			data, ok := m.store[path]
			if !ok {
				http.Error(w, `{"errors":["not found"]}`, http.StatusNotFound)
				return
			}
			out := map[string]interface{}{
				"request_id": reqID,
				"data": map[string]interface{}{
					"data": data,
					"metadata": map[string]interface{}{
						"created_time": time.Now().UTC().Format(time.RFC3339Nano),
						"version":      1,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/data/"):
			path := strings.TrimPrefix(r.URL.Path, "/v1/")
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Data map[string]string `json:"data"`
			}
			_ = json.Unmarshal(body, &payload)
			m.store[path] = payload.Data
			out := map[string]interface{}{"request_id": reqID, "data": map[string]interface{}{"version": 1}}
			_ = json.NewEncoder(w).Encode(out)
			return
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/metadata/"):
			path := strings.TrimPrefix(r.URL.Path, "/v1/")
			path = strings.Replace(path, "/metadata/", "/data/", 1)
			delete(m.store, path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "mock vault: unhandled "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
	})
}

func nextID(n int) string { return string(rune('a' + (n % 26))) }

// ── Tests ──────────────────────────────────────────────────────────────

func TestVaultStore_NewVaultStore_Validations(t *testing.T) {
	cases := map[string]struct {
		address, authMethod, token, layout string
		wantErr string
	}{
		"missing address":   {"", "token", "x", "flat", "address is required"},
		"unsupported auth":  {"https://v", "approle", "x", "flat", "BL281"},
		"missing token":     {"https://v", "token", "", "flat", "token is required"},
		"bad path layout":   {"https://v", "token", "x", "weird", "path_layout"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewVaultStore(c.address, "", c.authMethod, c.token, "secret", "", c.layout, "", false, 0)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("want err containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestVaultStore_RoundTrip_Flat(t *testing.T) {
	mv := newMockVault(t, "test-token", "")
	srv := httptest.NewServer(mv.handler())
	defer srv.Close()

	st, err := NewVaultStore(srv.URL, "", "token", "test-token", "secret", "datawatch", "flat", "", false, 5*time.Second)
	if err != nil {
		t.Fatalf("NewVaultStore: %v", err)
	}

	// Set
	if err := st.Set("api-key", "s3cr3t", []string{"git", "github"}, "GitHub PAT", []string{"agents"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get round-trip
	sec, err := st.Get("api-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sec.Value != "s3cr3t" {
		t.Errorf("value: got %q want s3cr3t", sec.Value)
	}
	if sec.Description != "GitHub PAT" {
		t.Errorf("description: got %q", sec.Description)
	}
	if len(sec.Tags) != 2 || sec.Tags[0] != "git" {
		t.Errorf("tags round-trip lost: %v", sec.Tags)
	}
	if len(sec.Scopes) != 1 || sec.Scopes[0] != "agents" {
		t.Errorf("scopes round-trip lost: %v", sec.Scopes)
	}
	if sec.Backend != "vault" {
		t.Errorf("backend: got %q", sec.Backend)
	}

	// Exists
	if ok, err := st.Exists("api-key"); err != nil || !ok {
		t.Errorf("Exists(api-key) = (%v, %v); want (true, nil)", ok, err)
	}
	if ok, err := st.Exists("missing"); err != nil || ok {
		t.Errorf("Exists(missing) = (%v, %v); want (false, nil)", ok, err)
	}

	// List
	if err := st.Set("other", "v2", nil, "", nil); err != nil {
		t.Fatalf("Set other: %v", err)
	}
	items, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("List count: got %d want 2 (%v)", len(items), items)
	}

	// Delete
	if err := st.Delete("api-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Get("api-key"); err != ErrSecretNotFound {
		t.Errorf("post-delete Get: want ErrSecretNotFound got %v", err)
	}

	// Status reflects last successful op.
	stat := st.Status()
	if !stat.Reachable {
		t.Errorf("Status should be reachable after successful ops")
	}
	if stat.LastRequestID == "" {
		t.Errorf("LastRequestID should be populated for audit cross-reference")
	}
	if stat.PathLayout != "flat" || stat.KVMount != "secret" || stat.PathPrefix != "datawatch" {
		t.Errorf("status fields unexpected: %+v", stat)
	}
}

func TestVaultStore_TokenAndNamespaceHeaders(t *testing.T) {
	mv := newMockVault(t, "right-token", "ent-namespace")
	srv := httptest.NewServer(mv.handler())
	defer srv.Close()

	// Wrong token → 403 surfaced as transport error.
	st, _ := NewVaultStore(srv.URL, "ent-namespace", "token", "wrong-token", "secret", "", "flat", "", false, 5*time.Second)
	if err := st.Set("foo", "bar", nil, "", nil); err == nil {
		t.Errorf("Set with bad token should fail, got nil")
	}

	// Right token + missing namespace → 403.
	st2, _ := NewVaultStore(srv.URL, "", "token", "right-token", "secret", "", "flat", "", false, 5*time.Second)
	if err := st2.Set("foo", "bar", nil, "", nil); err == nil {
		t.Errorf("Set with missing required namespace should fail, got nil")
	}

	// Correct token + namespace → success.
	st3, _ := NewVaultStore(srv.URL, "ent-namespace", "token", "right-token", "secret", "", "flat", "", false, 5*time.Second)
	if err := st3.Set("foo", "bar", nil, "", nil); err != nil {
		t.Errorf("Set with correct token+ns: %v", err)
	}
}

func TestVaultStore_CheckHealth(t *testing.T) {
	mv := newMockVault(t, "test-token", "")
	srv := httptest.NewServer(mv.handler())
	defer srv.Close()

	st, _ := NewVaultStore(srv.URL, "", "token", "test-token", "secret", "", "flat", "", false, 5*time.Second)
	if err := st.CheckHealth(); err != nil {
		t.Errorf("CheckHealth: %v", err)
	}
}

func TestVaultStore_TagAware_Layout(t *testing.T) {
	mv := newMockVault(t, "test-token", "")
	srv := httptest.NewServer(mv.handler())
	defer srv.Close()

	st, _ := NewVaultStore(srv.URL, "", "token", "test-token", "secret", "datawatch", "tag_aware", "", false, 5*time.Second)
	// Write with first tag = git
	if err := st.Set("repo-token", "abc", []string{"git", "github"}, "", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Path should include the tag sub-folder.
	wantPath := "secret/data/datawatch/git/repo-token"
	if _, ok := mv.store[wantPath]; !ok {
		t.Errorf("tag_aware Set should write to %q, store has: %v", wantPath, mapKeys(mv.store))
	}
	// Get walks the layout to find the secret.
	sec, err := st.Get("repo-token")
	if err != nil {
		t.Fatalf("Get tag_aware: %v", err)
	}
	if sec.Value != "abc" {
		t.Errorf("Get value: %q", sec.Value)
	}
}

func mapKeys(m map[string]map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
