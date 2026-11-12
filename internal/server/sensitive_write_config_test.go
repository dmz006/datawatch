package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// allow_sensitive_writes round-trips through PUT/GET /api/config: a fresh
// config reports the default (enabled), and a PUT of false sticks and is
// reported back.
func TestSensitiveWritesConfigRoundTrip(t *testing.T) {
	s := &Server{
		cfg:     &config.Config{},
		cfgPath: t.TempDir() + "/config.yaml",
	}

	getAllow := func(t *testing.T) any {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d", rr.Code)
		}
		var got struct {
			MCP struct {
				AllowSensitiveWrites any `json:"allow_sensitive_writes"`
			} `json:"mcp"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		return got.MCP.AllowSensitiveWrites
	}

	if v := getAllow(t); v != true {
		t.Fatalf("default GET allow_sensitive_writes = %v, want true", v)
	}

	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config",
		strings.NewReader(`{"mcp.allow_sensitive_writes":false}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.MCP.AllowSensitiveWrites == nil || *s.cfg.MCP.AllowSensitiveWrites != false {
		t.Fatalf("cfg AllowSensitiveWrites = %v, want explicit false", s.cfg.MCP.AllowSensitiveWrites)
	}
	if v := getAllow(t); v != false {
		t.Fatalf("GET after PUT false: allow_sensitive_writes = %v, want false", v)
	}

	// Re-enabling sticks too.
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config",
		strings.NewReader(`{"mcp.allow_sensitive_writes":true}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT true: want 200, got %d", rr.Code)
	}
	if v := getAllow(t); v != true {
		t.Fatalf("GET after PUT true: allow_sensitive_writes = %v, want true", v)
	}
}
