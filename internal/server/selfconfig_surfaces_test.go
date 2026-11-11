// BL412 — self-config group + audit view: the REST surfaces.
//
//   PUT /api/config then GET /api/config must round-trip the three
//   mcp.self-config keys, and GET /api/audit/self-config must read the
//   JSONL the shared audit sink writes to (newest-first, corrupt lines
//   skipped, no 5xx).

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
	"github.com/dmz006/datawatch/internal/federation"
)

// The three self-config keys round-trip through PUT/GET /api/config, and a
// fresh config reports the default posture: enabled, denied-recording on.
func TestConfigRoundTrip_SelfConfig(t *testing.T) {
	s := &Server{
		cfg:     &config.Config{},
		cfgPath: t.TempDir() + "/config.yaml",
	}

	// Defaults with nothing set: the gate is ON and denied attempts are
	// recorded (absent = on is the operator-confirmed BL412 posture).
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	checkMCP := func(t *testing.T, body []byte, wantAllow, wantDenied any, wantPath string) {
		t.Helper()
		var got struct {
			MCP struct {
				AllowSelfConfig     any    `json:"allow_self_config"`
				SelfConfigAuditPath string `json:"self_config_audit_path"`
				SelfConfigDenied    any    `json:"self_config_audit_denied"`
			} `json:"mcp"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		if got.MCP.AllowSelfConfig != wantAllow {
			t.Errorf("GET allow_self_config = %v, want %v", got.MCP.AllowSelfConfig, wantAllow)
		}
		if got.MCP.SelfConfigDenied != wantDenied {
			t.Errorf("GET self_config_audit_denied = %v, want %v", got.MCP.SelfConfigDenied, wantDenied)
		}
		if got.MCP.SelfConfigAuditPath != wantPath {
			t.Errorf("GET self_config_audit_path = %q, want %q", got.MCP.SelfConfigAuditPath, wantPath)
		}
	}
	checkMCP(t, rr.Body.Bytes(), true, true, "")

	// Disabling the gate and the denied-recording must stick, and the
	// audit path must land.
	patch := `{"mcp.allow_self_config":false,"mcp.self_config_audit_path":"/var/lib/datawatch/audit/self.jsonl","mcp.self_config_audit_denied":false}`
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(patch)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.MCP.AllowSelfConfig == nil || *s.cfg.MCP.AllowSelfConfig != false {
		t.Errorf("cfg AllowSelfConfig = %v, want explicit false", s.cfg.MCP.AllowSelfConfig)
	}
	if s.cfg.MCP.SelfConfigAuditDenied == nil || *s.cfg.MCP.SelfConfigAuditDenied != false {
		t.Errorf("cfg SelfConfigAuditDenied = %v, want explicit false", s.cfg.MCP.SelfConfigAuditDenied)
	}

	rr = httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET after PUT: want 200, got %d", rr.Code)
	}
	checkMCP(t, rr.Body.Bytes(), false, false, "/var/lib/datawatch/audit/self.jsonl")
}

// Re-enabling through PUT sticks too (the round trip is symmetric).
func TestConfigRoundTrip_SelfConfigReEnable(t *testing.T) {
	s := &Server{
		cfg:     &config.Config{},
		cfgPath: t.TempDir() + "/config.yaml",
	}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"mcp.allow_self_config":true}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d", rr.Code)
	}
	if s.cfg.MCP.AllowSelfConfig == nil || *s.cfg.MCP.AllowSelfConfig != true {
		t.Fatalf("cfg AllowSelfConfig = %v, want explicit true", s.cfg.MCP.AllowSelfConfig)
	}
}

func writeSelfConfigAudit(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	p := filepath.Join(dir, "config-self-modify.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}
	return p
}

// GET /api/audit/self-config reads the sink's JSONL: newest-first, corrupt
// lines skipped, the limit honoured, empty (never null) when there is no file.
func TestHandleSelfConfigAudit(t *testing.T) {
	rec := func(surface, tool string, allowed bool, reason string) string {
		b, _ := json.Marshal(federation.ConfigAuditRecord{ //nolint:errcheck
			Time: tool, Surface: surface, Tool: tool,
			Keys: []string{"mcp.usage.calculator.variant"}, Allowed: allowed, Reason: reason,
			Caller: "session:opencode-a3f2",
		})
		return string(b)
	}
	dir := t.TempDir()
	path := writeSelfConfigAudit(t, dir,
		rec("mcp", "config_set 1", true, ""),
		"not json {{{",
		rec("comm", "configure 3", false, "gate off: mcp.allow_self_config is false"),
	)
	s := &Server{cfg: &config.Config{}}
	s.cfg.MCP.SelfConfigAuditPath = path

	rr := httptest.NewRecorder()
	s.handleSelfConfigAudit(rr, httptest.NewRequest(http.MethodGet, "/api/audit/self-config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Count   int                            `json:"count"`
		Entries []federation.ConfigAuditRecord `json:"entries"`
		Path    string                         `json:"path"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Count != 2 || len(got.Entries) != 2 {
		t.Fatalf("count = %d (len %d), want 2 (corrupt line skipped)", got.Count, len(got.Entries))
	}
	if got.Entries[0].Time != "configure 3" {
		t.Errorf("entries[0] = %q, want the newest record first", got.Entries[0].Time)
	}
	if got.Entries[1].Time != "config_set 1" {
		t.Errorf("entries[1] = %q, want the older record", got.Entries[1].Time)
	}
	if got.Entries[0].Reason == "" {
		t.Errorf("denied record lost its reason")
	}
	if got.Path != path {
		t.Errorf("path = %q, want %q", got.Path, path)
	}

	// limit caps at the newest N.
	rr = httptest.NewRecorder()
	s.handleSelfConfigAudit(rr, httptest.NewRequest(http.MethodGet, "/api/audit/self-config?limit=1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET limit=1: want 200, got %d", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode limit=1: %v", err)
	}
	if got.Count != 1 || got.Entries[0].Time != "configure 3" {
		t.Errorf("limit=1: got %+v, want only the newest record", got.Entries)
	}

	// A bad limit is a 400, not a 5xx.
	rr = httptest.NewRecorder()
	s.handleSelfConfigAudit(rr, httptest.NewRequest(http.MethodGet, "/api/audit/self-config?limit=abc", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("limit=abc: want 400, got %d", rr.Code)
	}

	// Anything but GET is refused.
	rr = httptest.NewRecorder()
	s.handleSelfConfigAudit(rr, httptest.NewRequest(http.MethodPost, "/api/audit/self-config", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: want 405, got %d", rr.Code)
	}
}

// No audit file at all → 200 with an empty list, not an error and not null.
func TestHandleSelfConfigAudit_Empty(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	rr := httptest.NewRecorder()
	s.handleSelfConfigAudit(rr, httptest.NewRequest(http.MethodGet, "/api/audit/self-config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Count   int `json:"count"`
		Entries any `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Count != 0 {
		t.Errorf("count = %d, want 0", got.Count)
	}
	if got.Entries == nil {
		t.Errorf("entries = null, want an empty list")
	}
}

// The sink's own path wins over the configured one (the running default is
// authoritative; the config value is the fallback).
func TestSelfConfigAuditPath_SinkFirst(t *testing.T) {
	dir := t.TempDir()
	sinkPath := writeSelfConfigAudit(t, dir, `{"time":"t1","surface":"mcp","tool":"config_set","allowed":true}`)
	cfgPath := filepath.Join(dir, "other.jsonl")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}
	s := &Server{cfg: &config.Config{}}
	s.cfg.MCP.SelfConfigAuditPath = cfgPath
	s.configAudit = federation.NewConfigAuditor(sinkPath, true)
	if got := s.selfConfigAuditPath(); got != sinkPath {
		t.Errorf("path = %q, want the sink's %q", got, sinkPath)
	}
}
