package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TestPeerSilenceMCPConfigSet: config_set forwards observer.peers.silent_alert_minutes
// to /api/config as a number, including 0 (watcher off).
func TestPeerSilenceMCPConfigSet(t *testing.T) {
	const key = "observer.peers.silent_alert_minutes"
	d, s := newEscalationDaemon(t)
	for _, c := range []struct {
		value string
		want  float64
	}{{"9", 9}, {"0", 0}} {
		res, err := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": key, "value": c.value}))
		if err != nil {
			t.Fatalf("config_set: %v", err)
		}
		if text := res.Content[0].(mcpsdk.TextContent).Text; strings.Contains(text, "error") || strings.Contains(text, "denied") {
			t.Fatalf("config_set refused: %s", text)
		}
		d.mu.Lock()
		got := d.config[key]
		d.mu.Unlock()
		if got != c.want {
			t.Errorf("stored %#v, want %v", got, c.want)
		}
	}
}
