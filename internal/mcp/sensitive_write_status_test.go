package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// sensitiveWritesStubDaemon stands in for GET /api/config. Its mcp block is
// built from the fake config the same way the daemon builds it: the effective
// value of each tri-state gate, never the raw pointer.
func sensitiveWritesStubDaemon(t *testing.T, fake config.MCPConfig) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mcp": map[string]any{
				"allow_self_config":      fake.SelfConfigEnabled(),
				"allow_sensitive_writes": fake.SensitiveWritesEnabled(),
			},
		})
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}
}

// TestSensitiveWritesStatusReported checks that get_config reports
// allow_sensitive_writes next to allow_self_config, for an absent key
// (enabled), an explicit true and an explicit false, and that the two
// settings are reported independently of each other.
func TestSensitiveWritesStatusReported(t *testing.T) {
	cases := []struct {
		name          string
		fake          config.MCPConfig
		wantSelf      bool
		wantSensitive bool
	}{
		{"both absent", config.MCPConfig{}, true, true},
		{"sensitive true", config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(true)}, true, true},
		{"sensitive false", config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(false)}, true, false},
		{"self false only", config.MCPConfig{AllowSelfConfig: config.BoolPtr(false)}, false, true},
		{"both false", config.MCPConfig{
			AllowSelfConfig:      config.BoolPtr(false),
			AllowSensitiveWrites: config.BoolPtr(false),
		}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sensitiveWritesStubDaemon(t, c.fake)
			res, err := s.handleGetConfig(context.Background(), callReq(map[string]any{"section": "mcp"}))
			if err != nil || res == nil || res.IsError {
				t.Fatalf("get_config: err=%v res=%+v", err, res)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(automationText(t, res)), &got); err != nil {
				t.Fatalf("mcp section is not JSON: %v", err)
			}
			if v, ok := got["allow_sensitive_writes"].(bool); !ok || v != c.wantSensitive {
				t.Errorf("allow_sensitive_writes = %v (present=%v), want %v", got["allow_sensitive_writes"], ok, c.wantSensitive)
			}
			if v, ok := got["allow_self_config"].(bool); !ok || v != c.wantSelf {
				t.Errorf("allow_self_config = %v (present=%v), want %v", got["allow_self_config"], ok, c.wantSelf)
			}
		})
	}
}

// TestSensitiveWritesStatusInFullConfig checks the unfiltered get_config
// output also carries the setting inside the mcp section.
func TestSensitiveWritesStatusInFullConfig(t *testing.T) {
	s := sensitiveWritesStubDaemon(t, config.MCPConfig{AllowSensitiveWrites: config.BoolPtr(false)})
	res, err := s.handleGetConfig(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("get_config: err=%v res=%+v", err, res)
	}
	var full struct {
		MCP map[string]any `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(automationText(t, res)), &full); err != nil {
		t.Fatal(err)
	}
	if v, ok := full.MCP["allow_sensitive_writes"].(bool); !ok || v {
		t.Errorf("full config mcp.allow_sensitive_writes = %v (present=%v), want false", full.MCP["allow_sensitive_writes"], ok)
	}
}

// TestGetConfigDescriptionNamesSensitiveWrites checks the tool description
// tells a caller where to read the setting.
func TestGetConfigDescriptionNamesSensitiveWrites(t *testing.T) {
	d := (&Server{}).toolGetConfig().Description
	if !strings.Contains(d, "allow_sensitive_writes") || !strings.Contains(d, "allow_self_config") {
		t.Errorf("get_config description lacks the mcp gate settings: %q", d)
	}
}
