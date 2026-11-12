// inherited_tags_cache_seconds — Configuration Accessibility round-trip:
// PUT /api/config then GET /api/config must show the new value for
// autonomous.inherited_tags_cache_seconds, and the live session manager
// must receive the lifetime immediately (no restart).

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestConfigRoundTrip_InheritedTagsCacheSeconds(t *testing.T) {
	mgr, err := session.NewManager("inherited-tags-cache-test", t.TempDir(), "", 30*time.Second)
	if err != nil {
		t.Fatalf("new session manager: %v", err)
	}
	trackSessionManager(t, mgr)
	s := &Server{
		cfg:     &config.Config{},
		cfgPath: t.TempDir() + "/config.yaml",
		manager: mgr,
	}

	// Positive lifetime: PUT, then the config and the live manager agree.
	b, _ := json.Marshal(map[string]any{"autonomous.inherited_tags_cache_seconds": 90})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := s.cfg.Autonomous.InheritedTagsCacheSeconds; got != 90 {
		t.Fatalf("config inherited_tags_cache_seconds = %d, want 90", got)
	}
	if got := mgr.InheritedTagsCacheTTL(); got != 90*time.Second {
		t.Fatalf("manager inherited-tags cache TTL = %v, want 90s (live apply)", got)
	}

	// GET must round-trip the written value.
	rr = httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			InheritedTagsCacheSeconds int `json:"inherited_tags_cache_seconds"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.InheritedTagsCacheSeconds != 90 {
		t.Errorf("GET inherited_tags_cache_seconds = %d, want 90", got.Autonomous.InheritedTagsCacheSeconds)
	}

	// A negative value turns the cache off, and that must reach the manager too.
	b, _ = json.Marshal(map[string]any{"autonomous.inherited_tags_cache_seconds": -1})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT -1: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := mgr.InheritedTagsCacheTTL(); got != -1*time.Second {
		t.Fatalf("manager inherited-tags cache TTL after PUT -1 = %v, want -1s (caching off)", got)
	}

	// A non-integer value is ignored, not an error.
	b, _ = json.Marshal(map[string]any{"autonomous.inherited_tags_cache_seconds": "bogus"})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT non-integer: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := mgr.InheritedTagsCacheTTL(); got != -1*time.Second {
		t.Fatalf("manager TTL after non-integer PUT = %v, want unchanged -1s", got)
	}
}
