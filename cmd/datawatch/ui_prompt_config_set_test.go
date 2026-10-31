// Config-parity test for the four UI prompt handling keys on the CLI. The
// CLI keeps no key list of its own: datawatch config set sends PUT /api/config
// and config get reads GET /api/config for any dotted key, so
// session.dismiss_ui_prompts (bool) and the whole-number
// session.ui_prompt_dismiss_delay_seconds (>= 0; 0 = send at once),
// session.ui_prompt_clear_bound_seconds (>= 1) and
// session.ui_prompt_max_attempts (>= 1) need no CLI change. These tests pin
// that generic path: a boolean goes out as a real boolean, a whole number
// goes out as a number (not quoted on retry), a refused value reaches the
// operator with the daemon's text and stores nothing, and config get prints
// what the daemon holds.

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

// uiPromptCLIDaemon is a PUT/GET /api/config stand-in that, like the real
// daemon, answers 400 to a dismiss_ui_prompts value that is not a boolean
// and to a session.ui_prompt_* value that is not a whole number in range,
// and stores only what it accepted.
type uiPromptCLIDaemon struct {
	config     map[string]any
	configBody string
}

func startUIPromptCLIDaemon(t *testing.T) *uiPromptCLIDaemon {
	t.Helper()
	d := &uiPromptCLIDaemon{config: map[string]any{}}
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

// TestUIPromptSettings_CLISetKeys: config set sends dismiss_ui_prompts as a
// real boolean and each ui_prompt_* key as a whole number (never quoted); a
// later value overwrites an earlier one.
func TestUIPromptSettings_CLISetKeys(t *testing.T) {
	d := startUIPromptCLIDaemon(t)
	cfg := &config.Config{}
	cases := []struct {
		key, value string
		want       any
	}{
		{"session.dismiss_ui_prompts", "false", false},
		{"session.dismiss_ui_prompts", "true", true},
		{"session.ui_prompt_dismiss_delay_seconds", "0", float64(0)},
		{"session.ui_prompt_dismiss_delay_seconds", "45", float64(45)},
		{"session.ui_prompt_clear_bound_seconds", "60", float64(60)},
		{"session.ui_prompt_max_attempts", "5", float64(5)},
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

// TestUIPromptSettings_CLIBadValuesRefused: a wrong-shape dismiss value and
// an out-of-range or non-whole number fail config set with the daemon's text
// (naming the key) and store nothing.
func TestUIPromptSettings_CLIBadValuesRefused(t *testing.T) {
	d := startUIPromptCLIDaemon(t)
	cfg := &config.Config{}
	cases := []struct {
		key, value string
	}{
		{"session.dismiss_ui_prompts", "abc"},
		{"session.ui_prompt_dismiss_delay_seconds", "-1"},
		{"session.ui_prompt_clear_bound_seconds", "0"},
		{"session.ui_prompt_max_attempts", "0"},
		{"session.ui_prompt_max_attempts", "-2"},
	}
	for _, c := range cases {
		_, err := captureEscalationStdout(t, func() error {
			return runConfigSet(cfg, c.key, c.value)
		})
		if err == nil {
			t.Fatalf("config set %s = %s: want a refusal, got none", c.key, c.value)
		}
		if frag := strings.TrimPrefix(c.key, "session."); !strings.Contains(err.Error(), frag) {
			t.Errorf("refusal for %s should name the key; got %v", c.key, err)
		}
		if _, stored := d.config[c.key]; stored {
			t.Fatalf("a refused value %s was stored: %#v", c.value, d.config[c.key])
		}
	}
}

// TestUIPromptSettings_CLIGetReadsKeys: config get prints the value the
// daemon holds for each key.
func TestUIPromptSettings_CLIGetReadsKeys(t *testing.T) {
	d := startUIPromptCLIDaemon(t)
	d.configBody = `{"session":{"dismiss_ui_prompts":false,
		"ui_prompt_dismiss_delay_seconds":0,"ui_prompt_clear_bound_seconds":60,
		"ui_prompt_max_attempts":5}}`
	cfg := &config.Config{}
	want := map[string]string{
		"session.dismiss_ui_prompts":              "false",
		"session.ui_prompt_dismiss_delay_seconds": "0",
		"session.ui_prompt_clear_bound_seconds":   "60",
		"session.ui_prompt_max_attempts":          "5",
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
