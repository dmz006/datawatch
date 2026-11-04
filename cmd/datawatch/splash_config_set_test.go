// Config-parity test for the two UI splash minimum-dwell keys on the CLI.
// The CLI keeps no key list of its own: datawatch config set sends
// PUT /api/config and config get reads GET /api/config for any dotted key,
// so ui.splash_min_seconds (whole seconds 0-60; 0 = keep the legacy timing,
// default 6) and ui.splash_min_seconds_new_version (same shape, default 9)
// need no CLI change. These tests pin that generic path: a whole number
// goes out as a number, a refused value reaches the operator with the
// daemon's text and stores nothing, and config get prints what the daemon
// holds.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// splashCLIDaemon is a PUT/GET /api/config stand-in that, like the real
// daemon, answers 400 to a ui.splash_* value that is not a whole number from
// 0 to 60, and stores only what it accepted.
type splashCLIDaemon struct {
	config     map[string]any
	configBody string
}

func startSplashCLIDaemon(t *testing.T) *splashCLIDaemon {
	t.Helper()
	d := &splashCLIDaemon{config: map[string]any{}}
	whole := func(key string, v any, validate func(int) error) string {
		var n int
		switch x := v.(type) {
		case int:
			n = x
		case float64:
			if x != float64(int(x)) {
				return key + ": must be a whole number"
			}
			n = int(x)
		default:
			return key + ": must be a whole number"
		}
		if err := validate(n); err != nil {
			return key + ": " + err.Error()
		}
		return ""
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(d.configBody))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		for key, validate := range map[string]func(int) error{
			"ui.splash_min_seconds":             config.ValidateUISplashMinSeconds,
			"ui.splash_min_seconds_new_version": config.ValidateUISplashMinSecondsNewVersion,
		} {
			if v, ok := body[key]; ok {
				if msg := whole(key, v, validate); msg != "" {
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
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return d
}

// TestUISplashSettings_CLISetKeys: config set sends each splash key as a
// whole number (never quoted); a later value overwrites an earlier one.
func TestUISplashSettings_CLISetKeys(t *testing.T) {
	d := startSplashCLIDaemon(t)
	cfg := &config.Config{}
	cases := []struct {
		key, value string
		want       any
	}{
		{"ui.splash_min_seconds", "45", float64(45)},
		{"ui.splash_min_seconds", "0", float64(0)},
		{"ui.splash_min_seconds", "60", float64(60)},
		{"ui.splash_min_seconds_new_version", "9", float64(9)},
		{"ui.splash_min_seconds_new_version", "0", float64(0)},
	}
	for _, c := range cases {
		if _, err := captureEscalationStdout(t, func() error {
			return runConfigSet(cfg, c.key, c.value)
		}); err != nil {
			t.Fatalf("config set %s = %s: %v", c.key, c.value, err)
		}
		if got := d.config[c.key]; got != c.want {
			t.Fatalf("stored %s = %#v, want %#v", c.key, got, c.want)
		}
	}
}

// TestUISplashSettings_CLIBadValuesRefused: a wrong-shape or out-of-range
// value fails config set with the daemon's text (naming the key) and stores
// nothing.
func TestUISplashSettings_CLIBadValuesRefused(t *testing.T) {
	d := startSplashCLIDaemon(t)
	cfg := &config.Config{}
	cases := []struct {
		key, value string
	}{
		{"ui.splash_min_seconds", "abc"},
		{"ui.splash_min_seconds", "-1"},
		{"ui.splash_min_seconds", "61"},
		{"ui.splash_min_seconds_new_version", "1.5"},
		{"ui.splash_min_seconds_new_version", "-2"},
		{"ui.splash_min_seconds_new_version", "61"},
	}
	for _, c := range cases {
		_, err := captureEscalationStdout(t, func() error {
			return runConfigSet(cfg, c.key, c.value)
		})
		if err == nil {
			t.Fatalf("config set %s = %s: want a refusal, got none", c.key, c.value)
		}
		if !strings.Contains(err.Error(), c.key) {
			t.Errorf("refusal for %s should name the key; got %v", c.key, err)
		}
		if _, stored := d.config[c.key]; stored {
			t.Fatalf("a refused value %s was stored: %#v", c.value, d.config[c.key])
		}
	}
}

// TestUISplashSettings_CLIGetReadsKeys: config get prints the value the
// daemon holds under ui for each key.
func TestUISplashSettings_CLIGetReadsKeys(t *testing.T) {
	d := startSplashCLIDaemon(t)
	d.configBody = `{"ui":{"splash_min_seconds":0,
		"splash_min_seconds_new_version":60}}`
	cfg := &config.Config{}
	want := map[string]string{
		"ui.splash_min_seconds":             "0",
		"ui.splash_min_seconds_new_version": "60",
	}
	for key, wantStr := range want {
		out, err := captureEscalationStdout(t, func() error {
			return runConfigGet(cfg, key)
		})
		if err != nil {
			t.Fatalf("config get %s: %v", key, err)
		}
		if got := strings.TrimSpace(out); got != wantStr {
			t.Errorf("config get %s printed %q, want %q", key, got, wantStr)
		}
	}
}
