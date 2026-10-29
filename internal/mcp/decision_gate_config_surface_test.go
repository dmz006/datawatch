// Config-parity test for the decision gate keys over MCP. The two keys,
// autonomous.questions.decision_gate (bool) and
// autonomous.questions.decision_gate_mode (shadow or on), are set through the
// generic config_set tool, which PUTs any dotted key to /api/config, exactly
// like the question channel switch and its notify toggles. The fixed-argument
// autonomous_config_set tool does not carry any autonomous.questions.* key
// (the channel switch is not on it either), so it is left unchanged. The
// daemon stand-in below refuses what the real daemon refuses (invalid JSON,
// a non-boolean gate, a mode other than shadow or on) and stores only what it
// accepted.
package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type decisionGateMCPDaemon struct {
	mu     sync.Mutex
	config map[string]any
}

func newDecisionGateMCPServer(t *testing.T) (*decisionGateMCPDaemon, *Server) {
	t.Helper()
	d := &decisionGateMCPDaemon{config: map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if v, ok := body["autonomous.questions.decision_gate"]; ok {
			if _, isBool := v.(bool); !isBool {
				http.Error(w, "autonomous.questions.decision_gate: must be a boolean (true or false)", http.StatusBadRequest)
				return
			}
		}
		if v, ok := body["autonomous.questions.decision_gate_mode"]; ok {
			mode, isStr := v.(string)
			if !isStr {
				http.Error(w, "autonomous.questions.decision_gate_mode: must be a string", http.StatusBadRequest)
				return
			}
			if err := config.ValidateDecisionGateMode(mode); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		for k, v := range body {
			d.config[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return d, &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: true}}
}

func decisionGateConfigSet(t *testing.T, s *Server, key, value string) string {
	t.Helper()
	res, err := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": key, "value": value}))
	if err != nil {
		t.Fatalf("config_set %s = %s: %v", key, value, err)
	}
	return res.Content[0].(mcpsdk.TextContent).Text
}

// TestDecisionGateConfigSurface_SetGateAndMode: config_set carries the gate
// as a real boolean and the mode as a string; a later value overwrites an
// earlier one.
func TestDecisionGateConfigSurface_SetGateAndMode(t *testing.T) {
	d, s := newDecisionGateMCPServer(t)
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.questions.decision_gate", "false", false},
		{"autonomous.questions.decision_gate", "true", true},
		{"autonomous.questions.decision_gate_mode", "on", "on"},
		{"autonomous.questions.decision_gate_mode", "shadow", "shadow"},
	}
	for _, c := range cases {
		if text := decisionGateConfigSet(t, s, c.key, c.value); strings.Contains(text, "error") || strings.Contains(text, "denied") {
			t.Fatalf("config_set %s = %s refused: %s", c.key, c.value, text)
		}
		d.mu.Lock()
		got, ok := d.config[c.key]
		d.mu.Unlock()
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("stored %s = %#v (present=%v), want %#v", c.key, got, ok, c.want)
		}
	}
}

// TestDecisionGateConfigSurface_BadModeRefused: a mode other than shadow or on
// comes back as an error carrying the daemon's text, and nothing is stored.
func TestDecisionGateConfigSurface_BadModeRefused(t *testing.T) {
	d, s := newDecisionGateMCPServer(t)
	const key = "autonomous.questions.decision_gate_mode"
	for _, bad := range []string{"bogus", "ON", "off", "1"} {
		text := decisionGateConfigSet(t, s, key, bad)
		if !strings.Contains(text, "error") || !strings.Contains(text, "decision_gate_mode") {
			t.Fatalf("config_set %s = %s: want a refusal naming the key, got %q", key, bad, text)
		}
		d.mu.Lock()
		_, stored := d.config[key]
		d.mu.Unlock()
		if stored {
			t.Fatalf("a refused mode %s was stored", bad)
		}
	}
}
