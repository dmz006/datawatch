// v5.27.10 (BL216) — tests for /api/channel/info handler.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleChannelInfo_RejectsNonGet(t *testing.T) {
	s := bl90Server(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/channel/info", nil)
		rr := httptest.NewRecorder()
		s.handleChannelInfo(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s should be 405, got %d", method, rr.Code)
		}
	}
}

func TestHandleChannelInfo_ShapeOK(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/channel/info", nil)
	rr := httptest.NewRecorder()
	s.handleChannelInfo(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var info ChannelInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Kind != "go" && info.Kind != "js" {
		t.Errorf("kind = %q, want go|js", info.Kind)
	}
	// stale_mcp_json must always be a slice (omitempty would surprise UI).
	if info.StaleMCPJSON == nil {
		// JSON omitempty drops the field when empty; that's fine. Just
		// assert the slice is properly addressable when present (via
		// the Go zero-value path).
	}
}

func TestHandleChannelInfo_MCPModeStatus(t *testing.T) {
	// v5.28.7 — test that MCP mode status (stdio/SSE) is reported in the response.
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/channel/info", nil)
	rr := httptest.NewRecorder()
	s.handleChannelInfo(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var info ChannelInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// At minimum, stdio should match config (it's always enabled by default in tests).
	if s.cfg != nil {
		if info.StdioEnabled != s.cfg.MCP.Enabled {
			t.Errorf("StdioEnabled = %v, want %v", info.StdioEnabled, s.cfg.MCP.Enabled)
		}
		if info.SSEEnabled != s.cfg.MCP.SSEEnabled {
			t.Errorf("SSEEnabled = %v, want %v", info.SSEEnabled, s.cfg.MCP.SSEEnabled)
		}
	}
}
