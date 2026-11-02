// session.dismiss_ui_prompts and the three session.ui_prompt_* keys over the
// config API: GET /api/config returns the effective values, a PUT stores them,
// reaches config.yaml and is picked up by a manager wired to the same config
// with no restart, and a bad value is a 400 that names the key and changes
// nothing.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func getSessionUIPrompt(t *testing.T, s *Server) session.UIPromptSettings {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Session struct {
			Dismiss  *bool `json:"dismiss_ui_prompts"`
			Delay    *int  `json:"ui_prompt_dismiss_delay_seconds"`
			Bound    *int  `json:"ui_prompt_clear_bound_seconds"`
			Attempts *int  `json:"ui_prompt_max_attempts"`
		} `json:"session"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	g := got.Session
	if g.Dismiss == nil || g.Delay == nil || g.Bound == nil || g.Attempts == nil {
		t.Fatalf("GET /api/config lacks session UI prompt keys: %+v", g)
	}
	return session.UIPromptSettings{
		Enabled:           *g.Dismiss,
		DelaySeconds:      *g.Delay,
		ClearBoundSeconds: *g.Bound,
		MaxAttempts:       *g.Attempts,
	}
}

func putUIPromptRaw(t *testing.T, s *Server, key, rawValue string) (int, string) {
	t.Helper()
	body := bytes.NewBufferString(`{"` + key + `": ` + rawValue + `}`)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", body))
	return rr.Code, rr.Body.String()
}

func TestUIPromptSettings_ConfigAPI(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	// Defaults when unset.
	def := getSessionUIPrompt(t, s)
	if !def.Enabled || def.DelaySeconds != 5 || def.ClearBoundSeconds != 30 || def.MaxAttempts != 2 {
		t.Fatalf("GET defaults = %+v, want {true 5 30 2}", def)
	}
	if s.cfg.Session.DismissUIPrompts != nil || s.cfg.Session.UIPromptDismissDelaySeconds != nil ||
		s.cfg.Session.UIPromptClearBoundSeconds != nil || s.cfg.Session.UIPromptMaxAttempts != nil {
		t.Fatalf("stored values must be absent by default: %+v", s.cfg.Session)
	}

	// Manager wired the way main.go does it: the getter reads the same
	// config the server mutates.
	mgr := &session.Manager{}
	mgr.SetUIPromptSettings(func() session.UIPromptSettings {
		return session.UIPromptSettings{
			Enabled:           s.cfg.Session.EffectiveDismissUIPrompts(),
			DelaySeconds:      s.cfg.Session.EffectiveUIPromptDismissDelaySeconds(),
			ClearBoundSeconds: s.cfg.Session.EffectiveUIPromptClearBoundSeconds(),
			MaxAttempts:       s.cfg.Session.EffectiveUIPromptMaxAttempts(),
		}
	})
	if live := mgr.UIPromptSettings(); live != def {
		t.Fatalf("manager default = %+v, want %+v", live, def)
	}

	// Explicit values survive a PUT, a GET and reach config.yaml, and the
	// manager sees them with no restart.
	puts := []struct {
		key, raw string
	}{
		{"session.dismiss_ui_prompts", "false"},
		{"session.ui_prompt_dismiss_delay_seconds", "0"},
		{"session.ui_prompt_clear_bound_seconds", "45"},
		{"session.ui_prompt_max_attempts", "3"},
	}
	for _, p := range puts {
		if code, body := putUIPromptRaw(t, s, p.key, p.raw); code != http.StatusOK {
			t.Fatalf("PUT %s=%s: want 200, got %d %s", p.key, p.raw, code, body)
		}
	}
	got := getSessionUIPrompt(t, s)
	if got != (session.UIPromptSettings{Enabled: false, DelaySeconds: 0, ClearBoundSeconds: 45, MaxAttempts: 3}) {
		t.Fatalf("GET after PUTs = %+v, want {false 0 45 3}", got)
	}
	if live := mgr.UIPromptSettings(); live != got {
		t.Fatalf("manager after PUTs = %+v, want %+v (no restart)", live, got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, wantLine := range []string{"dismiss_ui_prompts: false", "ui_prompt_dismiss_delay_seconds: 0", "ui_prompt_clear_bound_seconds: 45", "ui_prompt_max_attempts: 3"} {
		if !strings.Contains(string(raw), wantLine) {
			t.Errorf("config.yaml lacks %q:\n%s", wantLine, raw)
		}
	}

	// Each bad value is refused, names the key, and changes nothing.
	bad := []struct {
		key    string
		values []string
	}{
		{"session.dismiss_ui_prompts", []string{`"abc"`, `1`, `null`}},
		{"session.ui_prompt_dismiss_delay_seconds", []string{`"5"`, `1.5`, `null`, `true`}},
		{"session.ui_prompt_clear_bound_seconds", []string{`0`, `-1`, `"30"`, `1.5`, `null`}},
		{"session.ui_prompt_max_attempts", []string{`0`, `-2`, `"2"`, `1.5`, `null`}},
	}
	for _, b := range bad {
		for _, v := range b.values {
			code, body := putUIPromptRaw(t, s, b.key, v)
			if code != http.StatusBadRequest {
				t.Errorf("PUT %s=%s: want 400, got %d %s", b.key, v, code, body)
				continue
			}
			if !strings.Contains(body, b.key) {
				t.Errorf("PUT %s=%s: message %q should name the key", b.key, v, body)
			}
			if got := getSessionUIPrompt(t, s); got != (session.UIPromptSettings{Enabled: false, DelaySeconds: 0, ClearBoundSeconds: 45, MaxAttempts: 3}) {
				t.Errorf("after refused PUT %s=%s the values are %+v (want unchanged)", b.key, v, got)
			}
		}
	}
}
