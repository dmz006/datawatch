// Config-parity test for the worker question marker switch: the generic MCP
// config tool must pass autonomous.questions.worker_marker through as a real
// boolean, and an explicit false must overwrite an explicit true in the
// daemon's stored config.
package mcp

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestQuestionWorkerMarkerConfig_MCPSetSwitch(t *testing.T) {
	const key = "autonomous.questions.worker_marker"
	d, s := newEscalationDaemon(t)
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
