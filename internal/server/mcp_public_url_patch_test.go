// GH#207: mcp.public_url must be writable through PUT /api/config
// (applyConfigPatch), with surrounding whitespace and trailing slashes
// trimmed, and clearable back to empty.

package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestApplyConfigPatch_MCPPublicURL(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"mcp.public_url": " https://host.tailnet.ts.net:8081/ "})
	if got, want := cfg.MCP.PublicURL, "https://host.tailnet.ts.net:8081"; got != want {
		t.Errorf("mcp.public_url = %q, want %q", got, want)
	}

	applyConfigPatch(cfg, map[string]interface{}{"mcp.public_url": ""})
	if cfg.MCP.PublicURL != "" {
		t.Errorf("mcp.public_url not cleared, got %q", cfg.MCP.PublicURL)
	}
}
