// BL390 (v8.38.0) — council config `backends` field REST round-trip.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestBL390_CouncilConfig_BackendsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	srv := &Server{cfg: cfg, cfgPath: cfgPath}

	// PATCH backends.
	body := `{"backends":["ollama-datawatch","claude-code"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/council/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleCouncilConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var patchResp struct {
		Backends []string `json:"backends"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &patchResp); err != nil {
		t.Fatalf("decode PATCH response: %v", err)
	}
	if len(patchResp.Backends) != 2 || patchResp.Backends[0] != "ollama-datawatch" || patchResp.Backends[1] != "claude-code" {
		t.Errorf("PATCH response backends = %v, want [ollama-datawatch claude-code]", patchResp.Backends)
	}
	if len(cfg.Council.Backends) != 2 {
		t.Errorf("cfg.Council.Backends = %v after PATCH, want 2 entries", cfg.Council.Backends)
	}

	// GET should reflect the same value.
	getReq := httptest.NewRequest(http.MethodGet, "/api/council/config", nil)
	getRR := httptest.NewRecorder()
	srv.handleCouncilConfig(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRR.Code, getRR.Body.String())
	}
	var getResp struct {
		Backends []string `json:"backends"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if len(getResp.Backends) != 2 {
		t.Errorf("GET backends = %v, want 2 entries matching the PATCH", getResp.Backends)
	}

	// Reloading from disk confirms it actually persisted, not just an
	// in-memory mutation.
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if len(reloaded.Council.Backends) != 2 {
		t.Errorf("reloaded cfg.Council.Backends = %v, want 2 entries persisted to disk", reloaded.Council.Backends)
	}
}
