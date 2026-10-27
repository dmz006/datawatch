// Acknowledge-unauthenticated setting: round-trip through PUT/GET
// /api/config and locale-key presence in every bundle.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func listenerAuthTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	s.cfg = config.DefaultConfig()
	s.cfgPath = filepath.Join(t.TempDir(), "config.yaml")
	return s
}

// TestListenerAuth_ConfigPutGetRoundTrip PUTs the key through the same
// handler chain a REST client or the comm channel uses, then GETs it
// back and checks the in-memory value each time.
func TestListenerAuth_ConfigPutGetRoundTrip(t *testing.T) {
	s := listenerAuthTestServer(t)
	if s.cfg.Server.AcknowledgeUnauthenticated {
		t.Fatal("default must be false")
	}

	roundTrip := func(want bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]interface{}{"server.acknowledge_unauthenticated": want})
		req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin-token")
		rr := httptest.NewRecorder()
		s.fedAuthMiddleware(http.HandlerFunc(s.handleConfig)).ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT: status=%d body=%s", rr.Code, rr.Body.String())
		}
		if s.cfg.Server.AcknowledgeUnauthenticated != want {
			t.Fatalf("after PUT: in-memory value = %v, want %v", s.cfg.Server.AcknowledgeUnauthenticated, want)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		getReq.Header.Set("Authorization", "Bearer admin-token")
		rr = httptest.NewRecorder()
		s.fedAuthMiddleware(http.HandlerFunc(s.handleConfig)).ServeHTTP(rr, getReq)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: status=%d body=%s", rr.Code, rr.Body.String())
		}
		var v map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
			t.Fatalf("GET: decode: %v", err)
		}
		srv, _ := v["server"].(map[string]any)
		if got, ok := srv["acknowledge_unauthenticated"]; !ok || got != want {
			t.Fatalf("GET: server.acknowledge_unauthenticated = %v (present=%v), want %v", got, ok, want)
		}
	}

	roundTrip(true)
	roundTrip(false)
}

// TestListenerAuth_LocaleKeys_AllBundles — both settings-card keys must
// exist and be non-empty in every locale bundle.
func TestListenerAuth_LocaleKeys_AllBundles(t *testing.T) {
	keys := []string{
		"settings_acknowledge_unauthenticated",
		"settings_acknowledge_unauthenticated_help",
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range keys {
			v, ok := bundle[key]
			if !ok || v == "" {
				t.Errorf("locale %s: missing or empty key %s", lang, key)
			}
		}
	}
}
