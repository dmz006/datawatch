// Config-parity test for the operator-question channel. The channel's only
// runtime config value, autonomous.questions.enabled, must be reachable from
// the generic MCP config tool (Configuration-Accessibility rule: every config
// value is settable over every config surface without a restart) the same way
// autonomous.failure_escalation.* is in failure_escalation_surfaces_test.go —
// so pin the round trip: the tool PUTs the boolean to /api/config and the
// daemon's stored config carries it.
package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// TestQuestionsMCPConfigSetTogglesChannel: config_set passes the channel
// switch through as a real boolean.
func TestQuestionsMCPConfigSetTogglesChannel(t *testing.T) {
	d, s := newEscalationDaemon(t)
	req := subUpdateReq("config_set", map[string]any{
		"key":   "autonomous.questions.enabled",
		"value": "true",
	})
	res, err := s.handleConfigSet(context.Background(), req)
	if err != nil {
		t.Fatalf("config_set: %v", err)
	}
	if text := res.Content[0].(mcpsdk.TextContent).Text; strings.Contains(text, "error") || strings.Contains(text, "denied") {
		t.Fatalf("config_set autonomous.questions.enabled refused: %s", text)
	}
	d.mu.Lock()
	got, ok := d.config["autonomous.questions.enabled"]
	d.mu.Unlock()
	if !ok || got != true {
		t.Errorf("stored autonomous.questions.enabled = %#v (present=%v), want true", got, ok)
	}
}
