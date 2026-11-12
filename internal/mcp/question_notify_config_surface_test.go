// Config-parity test for the operator-question channel's notify toggles.
// Each of autonomous.questions.notify_messaging, .notify_alert and
// .return_path (all defaulting on when absent) must be settable from the
// generic MCP config tool the same way the channel switch and
// autonomous.failure_escalation.* are in failure_escalation_surfaces_test.go
// — so pin the round trip: the tool PUTs the boolean to /api/config and
// the daemon's stored config carries it.
package mcp

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func questionNotifySurfaceKeys() []string {
	return []string{
		"autonomous.questions.notify_messaging",
		"autonomous.questions.notify_alert",
		"autonomous.questions.return_path",
	}
}

// TestQuestionNotifyConfigSurface_SetToggles: config_set passes each notify
// toggle through as a real boolean, and an explicit false overwrites an
// explicit true in the daemon's stored config.
func TestQuestionNotifyConfigSurface_SetToggles(t *testing.T) {
	d, s := newEscalationDaemon(t)
	for _, key := range questionNotifySurfaceKeys() {
		for _, val := range []bool{true, false} {
			req := subUpdateReq("config_set", map[string]any{
				"key":   key,
				"value": strconv.FormatBool(val),
			})
			res, err := s.handleConfigSet(context.Background(), req)
			if err != nil {
				t.Fatalf("config_set %s = %v: %v", key, val, err)
			}
			if text := res.Content[0].(mcpsdk.TextContent).Text; strings.Contains(text, "error") || strings.Contains(text, "denied") {
				t.Fatalf("config_set %s = %v refused: %s", key, val, text)
			}
			d.mu.Lock()
			got, ok := d.config[key]
			d.mu.Unlock()
			if !ok || !reflect.DeepEqual(got, val) {
				t.Fatalf("stored %s = %#v (present=%v), want %v", key, got, ok, val)
			}
		}
	}
}
