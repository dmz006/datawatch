// pin_check_seconds (the pin keeper interval) on the REST config surface:
// GET /api/config returns it, PUT /api/config validates and stores it.
// Per-node pin fields are covered by internal/compute and the
// /api/compute/nodes handlers.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newPinCheckServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}, cfgPath
}

func getPinCheckSeconds(t *testing.T, s *Server) string {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return string(top["pin_check_seconds"])
}

func TestPinCheckSeconds_GetDefault300(t *testing.T) {
	s, _ := newPinCheckServer(t)
	if got := getPinCheckSeconds(t, s); got != "300" {
		t.Fatalf("GET pin_check_seconds = %q, want 300", got)
	}
}

func TestPinCheckSeconds_PutZeroSurvivesSaveAndLoad(t *testing.T) {
	s, cfgPath := newPinCheckServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"pin_check_seconds": 0})
	if s.cfg.PinCheckSeconds != 0 {
		t.Fatalf("s.cfg.PinCheckSeconds = %d, want 0", s.cfg.PinCheckSeconds)
	}
	if got := getPinCheckSeconds(t, s); got != "0" {
		t.Fatalf("GET after PUT 0 = %q, want 0", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "pin_check_seconds: 0") {
		t.Fatalf("config.yaml lacks explicit pin_check_seconds: 0:\n%s", raw)
	}
}

func TestPinCheckSeconds_PutAcceptsNumberAndNumericString(t *testing.T) {
	s, _ := newPinCheckServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"pin_check_seconds": 45})
	if got := getPinCheckSeconds(t, s); got != "45" {
		t.Fatalf("GET after numeric PUT = %q, want 45", got)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"pin_check_seconds": "120"})
	if got := getPinCheckSeconds(t, s); got != "120" {
		t.Fatalf("GET after string PUT = %q, want 120", got)
	}
}

func TestPinCheckSeconds_PutRejectsInvalidWithPlainMessage(t *testing.T) {
	cases := []any{-1, 1.5, "abc", "-5", true}
	for _, bad := range cases {
		s, _ := newPinCheckServer(t)
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"pin_check_seconds": 90})
		rr := httptest.NewRecorder()
		b, _ := json.Marshal(map[string]any{"pin_check_seconds": bad})
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v: want 400, got %d", bad, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "pin_check_seconds: must be a whole number") {
			t.Fatalf("PUT %v: message %q is not the plain pin_check_seconds message", bad, rr.Body.String())
		}
		if s.cfg.PinCheckSeconds != 90 {
			t.Fatalf("PUT %v changed the stored value to %d; a rejected PUT must leave it alone", bad, s.cfg.PinCheckSeconds)
		}
	}
}
