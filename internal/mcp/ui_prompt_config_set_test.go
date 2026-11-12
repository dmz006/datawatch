// Config-parity test for the four UI prompt handling keys over MCP:
// session.dismiss_ui_prompts (bool) and the whole-number
// session.ui_prompt_dismiss_delay_seconds (>= 0; 0 = send at once; default 5),
// session.ui_prompt_clear_bound_seconds (>= 1; default 30) and
// session.ui_prompt_max_attempts (>= 1; default 2). They travel through the
// generic config_set tool, which PUTs any dotted key to /api/config, exactly
// like autonomous.max_concurrent_tasks. The daemon stand-in below refuses
// what the real daemon refuses (a dismiss value that is not a real boolean,
// a non-whole or out-of-range number) and stores only what it accepted.

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

// uiPromptMCPDaemon is a PUT /api/config stand-in that, like the real daemon,
// answers 400 to a dismiss_ui_prompts value that is not a boolean and to a
// session.ui_prompt_* number that is not a whole number in range.
type uiPromptMCPDaemon struct {
	mu     sync.Mutex
	config map[string]any
}

func (d *uiPromptMCPDaemon) whole(key string, v any, validate func(int) error) string {
	n, ok := jsonNumber(v)
	if !ok {
		return key + ": must be a whole number"
	}
	if err := validate(n); err != nil {
		return key + ": " + err.Error()
	}
	return ""
}

// jsonNumber accepts the numeric encodings JSON can carry (float64 from
// encoding/json, int from json.Decoder) and requires a whole value.
func jsonNumber(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func newUIPromptMCPServer(t *testing.T) (*uiPromptMCPDaemon, *Server) {
	t.Helper()
	d := &uiPromptMCPDaemon{config: map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if v, ok := body["session.dismiss_ui_prompts"]; ok {
			if _, isBool := v.(bool); !isBool {
				http.Error(w, "session.dismiss_ui_prompts: must be a boolean (true or false)", http.StatusBadRequest)
				return
			}
		}
		for key, validate := range map[string]func(int) error{
			"session.ui_prompt_dismiss_delay_seconds": config.ValidateUIPromptDismissDelaySeconds,
			"session.ui_prompt_clear_bound_seconds":   config.ValidateUIPromptClearBoundSeconds,
			"session.ui_prompt_max_attempts":          config.ValidateUIPromptMaxAttempts,
		} {
			if v, ok := body[key]; ok {
				if msg := d.whole(key, v, validate); msg != "" {
					http.Error(w, msg, http.StatusBadRequest)
					return
				}
			}
		}
		for k, v := range body {
			d.config[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return d, &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
}

func uiPromptConfigSet(t *testing.T, s *Server, key, value string) string {
	t.Helper()
	res, err := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": key, "value": value}))
	if err != nil {
		t.Fatalf("config_set %s = %s: %v", key, value, err)
	}
	return res.Content[0].(mcpsdk.TextContent).Text
}

// TestUIPromptSettings_MCPSetKeys: config_set carries dismiss_ui_prompts as a
// real boolean and each ui_prompt_* key as a whole number; a later value
// overwrites an earlier one.
func TestUIPromptSettings_MCPSetKeys(t *testing.T) {
	d, s := newUIPromptMCPServer(t)
	cases := []struct {
		key, value string
		want       any
	}{
		{"session.dismiss_ui_prompts", "false", false},
		{"session.dismiss_ui_prompts", "true", true},
		{"session.ui_prompt_dismiss_delay_seconds", "45", float64(45)},
		{"session.ui_prompt_dismiss_delay_seconds", "0", float64(0)},
		{"session.ui_prompt_clear_bound_seconds", "60", float64(60)},
		{"session.ui_prompt_max_attempts", "5", float64(5)},
		{"session.ui_prompt_max_attempts", "1", float64(1)},
	}
	for _, c := range cases {
		if text := uiPromptConfigSet(t, s, c.key, c.value); strings.Contains(text, "error") || strings.Contains(text, "denied") {
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

// TestUIPromptSettings_MCPBadValuesRefused: a dismiss value of the wrong
// shape and an out-of-range or non-whole number come back as an error
// carrying the daemon's text, and nothing is stored.
func TestUIPromptSettings_MCPBadValuesRefused(t *testing.T) {
	d, s := newUIPromptMCPServer(t)
	cases := []struct {
		key, value string
		fragment   string
	}{
		{"session.dismiss_ui_prompts", "abc", "session.dismiss_ui_prompts"},
		{"session.ui_prompt_dismiss_delay_seconds", "-1", "session.ui_prompt_dismiss_delay_seconds"},
		{"session.ui_prompt_dismiss_delay_seconds", "1.5", "session.ui_prompt_dismiss_delay_seconds"},
		{"session.ui_prompt_clear_bound_seconds", "0", "session.ui_prompt_clear_bound_seconds"},
		{"session.ui_prompt_max_attempts", "0", "session.ui_prompt_max_attempts"},
		{"session.ui_prompt_max_attempts", "-2", "session.ui_prompt_max_attempts"},
	}
	for _, c := range cases {
		text := uiPromptConfigSet(t, s, c.key, c.value)
		if !strings.Contains(text, "error") || !strings.Contains(text, c.fragment) {
			t.Fatalf("config_set %s = %s: want a refusal naming the key, got %q", c.key, c.value, text)
		}
		d.mu.Lock()
		_, stored := d.config[c.key]
		d.mu.Unlock()
		if stored {
			t.Fatalf("a refused value %s for %s was stored", c.value, c.key)
		}
	}
}
