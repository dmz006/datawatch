// Config-parity test for the two UI splash minimum-dwell keys over MCP:
// ui.splash_min_seconds (whole seconds 0-60; 0 = keep the legacy timing;
// default 6) and ui.splash_min_seconds_new_version (same shape; default 9).
// They travel through the generic config_set tool, which PUTs any dotted key
// to /api/config, exactly like the session.ui_prompt_* keys. The daemon
// stand-in below refuses what the real daemon refuses (a non-whole or
// out-of-range number) and stores only what it accepted.

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

// splashMCPDaemon is a PUT /api/config stand-in that, like the real daemon,
// answers 400 to a ui.splash_* value that is not a whole number from 0 to 60.
type splashMCPDaemon struct {
	mu     sync.Mutex
	config map[string]any
}

func newSplashMCPServer(t *testing.T) (*splashMCPDaemon, *Server) {
	t.Helper()
	d := &splashMCPDaemon{config: map[string]any{}}
	validators := map[string]func(int) error{
		"ui.splash_min_seconds":             config.ValidateUISplashMinSeconds,
		"ui.splash_min_seconds_new_version": config.ValidateUISplashMinSecondsNewVersion,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for key, validate := range validators {
			if v, ok := body[key]; ok {
				n, whole := jsonNumber(v)
				if !whole {
					http.Error(w, key+": must be a whole number", http.StatusBadRequest)
					return
				}
				if err := validate(n); err != nil {
					http.Error(w, key+": "+err.Error(), http.StatusBadRequest)
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

func splashConfigSet(t *testing.T, s *Server, key, value string) string {
	t.Helper()
	res, err := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": key, "value": value}))
	if err != nil {
		t.Fatalf("config_set %s = %s: %v", key, value, err)
	}
	return res.Content[0].(mcpsdk.TextContent).Text
}

// TestUISplashSettings_MCPSetKeys: config_set carries each splash key as a
// whole number; a later value overwrites an earlier one.
func TestUISplashSettings_MCPSetKeys(t *testing.T) {
	d, s := newSplashMCPServer(t)
	cases := []struct {
		key, value string
		want       any
	}{
		{"ui.splash_min_seconds", "45", float64(45)},
		{"ui.splash_min_seconds", "0", float64(0)},
		{"ui.splash_min_seconds", "60", float64(60)},
		{"ui.splash_min_seconds_new_version", "9", float64(9)},
		{"ui.splash_min_seconds_new_version", "0", float64(0)},
		{"ui.splash_min_seconds_new_version", "60", float64(60)},
	}
	for _, c := range cases {
		if text := splashConfigSet(t, s, c.key, c.value); strings.Contains(text, "error") || strings.Contains(text, "denied") {
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

// TestUISplashSettings_MCPBadValuesRefused: a wrong-shape or out-of-range
// value comes back as an error carrying the daemon's text (naming the key),
// and nothing is stored.
func TestUISplashSettings_MCPBadValuesRefused(t *testing.T) {
	d, s := newSplashMCPServer(t)
	cases := []struct {
		key, value string
		fragment   string
	}{
		{"ui.splash_min_seconds", "abc", "ui.splash_min_seconds"},
		{"ui.splash_min_seconds", "1.5", "ui.splash_min_seconds"},
		{"ui.splash_min_seconds", "-1", "ui.splash_min_seconds"},
		{"ui.splash_min_seconds", "61", "ui.splash_min_seconds"},
		{"ui.splash_min_seconds_new_version", "abc", "ui.splash_min_seconds_new_version"},
		{"ui.splash_min_seconds_new_version", "-2", "ui.splash_min_seconds_new_version"},
		{"ui.splash_min_seconds_new_version", "61", "ui.splash_min_seconds_new_version"},
	}
	for _, c := range cases {
		text := splashConfigSet(t, s, c.key, c.value)
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
