package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestMCPConfigSetCarriesFinaliseInterval: config_set forwards the finalise
// interval to PUT /api/config under its dotted key, with the value as a JSON
// integer (not a quoted string).
func TestMCPConfigSetCarriesFinaliseInterval(t *testing.T) {
	const key = "session.finalise_interval_minutes"
	s, rec := activateServer(t, http.StatusOK, `{"status":"ok"}`)
	s.cfg = &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}

	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{"key": key, "value": "25"}))
	if err != nil {
		t.Fatalf("handleConfigSet: %v", err)
	}
	if text := automationText(t, res); !strings.Contains(text, "Set "+key+" = 25") {
		t.Errorf("config_set text = %q", text)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Fatalf("proxied %s %s, want PUT /api/config", rec.method, rec.path)
	}
	if len(rec.body) != 1 {
		t.Errorf("body has %d keys, want only %s: %v", len(rec.body), key, rec.body)
	}
	got, ok := rec.body[key]
	if !ok {
		t.Fatalf("body lacks dotted key %s: %v", key, rec.body)
	}
	if n, isNum := got.(float64); !isNum || n != 25 {
		t.Errorf("body[%s] = %#v, want the integer 25 as a JSON number", key, got)
	}
}
