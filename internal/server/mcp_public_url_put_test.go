// mcp.public_url over the REST config surface: PUT validates the value
// (400 and no change when invalid), stores a valid one trimmed, persists
// it to the config file, and GET /api/config reads it back.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newMCPPublicURLServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}, cfgPath
}

func putMCPPublicURL(s *Server, v any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{"mcp.public_url": v})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func TestPutConfig_MCPPublicURLRejectsInvalid(t *testing.T) {
	bad := []string{
		"ftp://x", "host:8081", "//host", "/relative", "javascript:alert(1)",
		"https://user:pw@host", "https://host?x=1", "https://host#f", "https://",
	}
	for _, v := range bad {
		t.Run(v, func(t *testing.T) {
			s, cfgPath := newMCPPublicURLServer(t)
			const good = "https://host.tailnet.ts.net:8081"
			if rr := putMCPPublicURL(s, good); rr.Code != http.StatusOK {
				t.Fatalf("PUT %q: want 200, got %d body=%s", good, rr.Code, rr.Body.String())
			}
			before, _ := os.ReadFile(cfgPath)
			rr := putMCPPublicURL(s, v)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT %q: want 400, got %d body=%s", v, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "mcp.public_url") {
				t.Errorf("PUT %q: message %q does not name mcp.public_url", v, rr.Body.String())
			}
			if s.cfg.MCP.PublicURL != good {
				t.Errorf("PUT %q changed the stored value to %q; a rejected PUT must leave it alone", v, s.cfg.MCP.PublicURL)
			}
			if after, _ := os.ReadFile(cfgPath); !bytes.Equal(before, after) {
				t.Errorf("PUT %q rewrote the config file", v)
			}
		})
	}
}

func TestPutConfig_MCPPublicURLRoundTrip(t *testing.T) {
	s, cfgPath := newMCPPublicURLServer(t)
	if rr := putMCPPublicURL(s, "  HTTPS://Host.Example.com:8081/  "); rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	const want = "HTTPS://Host.Example.com:8081"
	if s.cfg.MCP.PublicURL != want {
		t.Fatalf("stored = %q, want %q", s.cfg.MCP.PublicURL, want)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(raw), "public_url: "+want) {
		t.Errorf("config file lacks public_url: %s\n%s", want, raw)
	}

	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	var mcpCfg map[string]any
	if err := json.Unmarshal(top["mcp"], &mcpCfg); err != nil {
		t.Fatalf("decode GET mcp: %v", err)
	}
	if got := mcpCfg["public_url"]; got != want {
		t.Errorf("GET mcp.public_url = %v, want %q", got, want)
	}

	// Clearing it back to empty is allowed.
	if rr := putMCPPublicURL(s, "   "); rr.Code != http.StatusOK {
		t.Fatalf("PUT clear: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.MCP.PublicURL != "" {
		t.Errorf("not cleared, got %q", s.cfg.MCP.PublicURL)
	}
}
