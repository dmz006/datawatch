// ui.splash_min_seconds and ui.splash_min_seconds_new_version over the REST
// config API: GET /api/config returns the effective values under `ui`, a PUT
// stores them, reaches config.yaml, and is picked up by a getter wired to the
// same config with no restart, and a bad value is a 400 that names the key
// and changes nothing.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func getUISplash(t *testing.T, s *Server) config.UIConfig {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		UI struct {
			MinSeconds       *int `json:"splash_min_seconds"`
			MinSecondsNewVer *int `json:"splash_min_seconds_new_version"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.UI.MinSeconds == nil || got.UI.MinSecondsNewVer == nil {
		t.Fatalf("GET /api/config lacks ui splash keys: %+v", got.UI)
	}
	return config.UIConfig{
		SplashMinSeconds:           got.UI.MinSeconds,
		SplashMinSecondsNewVersion: got.UI.MinSecondsNewVer,
	}
}

func putUISplashRaw(t *testing.T, s *Server, key, rawValue string) (int, string) {
	t.Helper()
	body := bytes.NewBufferString(`{"` + key + `": ` + rawValue + `}`)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", body))
	return rr.Code, rr.Body.String()
}

func TestUISplashSettings_GetReturnsDefaultsWhenUnset(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	got := getUISplash(t, s)
	if got.SplashMinSeconds == nil || *got.SplashMinSeconds != config.DefaultUISplashMinSeconds {
		t.Errorf("GET ui.splash_min_seconds = %v, want default %d", got.SplashMinSeconds, config.DefaultUISplashMinSeconds)
	}
	if got.SplashMinSecondsNewVersion == nil || *got.SplashMinSecondsNewVersion != config.DefaultUISplashMinSecondsNewVersion {
		t.Errorf("GET ui.splash_min_seconds_new_version = %v, want default %d", got.SplashMinSecondsNewVersion, config.DefaultUISplashMinSecondsNewVersion)
	}
	if s.cfg.UI.SplashMinSeconds != nil || s.cfg.UI.SplashMinSecondsNewVersion != nil {
		t.Fatalf("stored values must be absent by default: %+v", s.cfg.UI)
	}
}

func TestUISplashSettings_PutRoundTripNoRestart(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	getter := func() (int, int) {
		return s.cfg.UI.SplashMinSecondsEffective(), s.cfg.UI.SplashMinSecondsNewVersionEffective()
	}
	if min, nov := getter(); min != config.DefaultUISplashMinSeconds || nov != config.DefaultUISplashMinSecondsNewVersion {
		t.Fatalf("getter default = (%d, %d), want (%d, %d)", min, nov,
			config.DefaultUISplashMinSeconds, config.DefaultUISplashMinSecondsNewVersion)
	}

	// Explicit values survive a PUT, a GET, reach config.yaml — and the
	// getter sees them with no restart.
	puts := []struct {
		key, raw string
	}{
		{"ui.splash_min_seconds", "45"},
		{"ui.splash_min_seconds", "0"}, // explicit 0 = keep the legacy timing
		{"ui.splash_min_seconds_new_version", "7"},
		{"ui.splash_min_seconds_new_version", "60"},
	}
	for _, p := range puts {
		if code, body := putUISplashRaw(t, s, p.key, p.raw); code != http.StatusOK {
			t.Fatalf("PUT %s=%s: want 200, got %d %s", p.key, p.raw, code, body)
		}
	}
	got := getUISplash(t, s)
	if *got.SplashMinSeconds != 0 || *got.SplashMinSecondsNewVersion != 60 {
		t.Fatalf("GET after PUTs = %+v, want {0 60}", got)
	}
	if min, nov := getter(); min != 0 || nov != 60 {
		t.Fatalf("getter after PUTs = (%d, %d), want (0, 60) with no restart", min, nov)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, wantLine := range []string{"splash_min_seconds: 0", "splash_min_seconds_new_version: 60"} {
		if !strings.Contains(string(raw), wantLine) {
			t.Errorf("config.yaml lacks %q:\n%s", wantLine, raw)
		}
	}

	// Both keys travel together in one PUT, like the other key groups.
	body := bytes.NewBufferString(`{"ui.splash_min_seconds": 3, "ui.splash_min_seconds_new_version": 4}`)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT both keys: want 200, got %d %s", rr.Code, rr.Body.String())
	}
	got = getUISplash(t, s)
	if *got.SplashMinSeconds != 3 || *got.SplashMinSecondsNewVersion != 4 {
		t.Fatalf("GET after combined PUT = %+v, want {3 4}", got)
	}
}

func TestUISplashSettings_BadValuesRefusedNamingTheKey(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	// Set known-good values first, then prove every refusal changes nothing.
	for _, p := range []struct{ key, raw string }{
		{"ui.splash_min_seconds", "5"},
		{"ui.splash_min_seconds_new_version", "6"},
	} {
		if code, body := putUISplashRaw(t, s, p.key, p.raw); code != http.StatusOK {
			t.Fatalf("PUT %s=%s: want 200, got %d %s", p.key, p.raw, code, body)
		}
	}
	bad := []struct {
		key    string
		values []string
	}{
		{"ui.splash_min_seconds", []string{`"5"`, `1.5`, `null`, `true`, `-1`, `61`}},
		{"ui.splash_min_seconds_new_version", []string{`"9"`, `1.5`, `null`, `true`, `-2`, `61`}},
	}
	for _, b := range bad {
		for _, v := range b.values {
			code, body := putUISplashRaw(t, s, b.key, v)
			if code != http.StatusBadRequest {
				t.Errorf("PUT %s=%s: want 400, got %d %s", b.key, v, code, body)
				continue
			}
			if !strings.Contains(body, b.key) {
				t.Errorf("PUT %s=%s: message %q should name the key", b.key, v, body)
			}
			if got := getUISplash(t, s); *got.SplashMinSeconds != 5 || *got.SplashMinSecondsNewVersion != 6 {
				t.Errorf("after refused PUT %s=%s the values are %+v (want unchanged {5 6})", b.key, v, got)
			}
		}
	}
}
