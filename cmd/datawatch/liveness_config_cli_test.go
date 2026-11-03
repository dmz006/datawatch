package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// livenessStubDaemon stands in for PUT/GET /api/config. Like the daemon it
// accepts the overrides as an object or as backend=seconds text, and refuses
// through config.ValidateLivenessSettings.
func livenessStubDaemon(t *testing.T) {
	t.Helper()
	var mu sync.Mutex
	window := 0
	overrides := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{
				"liveness_window_seconds":         window,
				"liveness_idle_timeout_overrides": overrides,
			}})
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var patch map[string]any
		if json.Unmarshal(raw, &patch) != nil {
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		nw, no := window, overrides
		if v, ok := patch["session.liveness_window_seconds"]; ok {
			f, isNum := v.(float64)
			if !isNum || f != float64(int(f)) {
				http.Error(w, "session.liveness_window_seconds: invalid; use whole seconds >= 0", http.StatusBadRequest)
				return
			}
			nw = int(f)
		}
		if v, ok := patch["session.liveness_idle_timeout_overrides"]; ok {
			no = map[string]int{}
			switch x := v.(type) {
			case map[string]any:
				for k, n := range x {
					f, _ := n.(float64)
					no[k] = int(f)
				}
			case string:
				for _, part := range strings.Split(x, ",") {
					k, val, found := strings.Cut(part, "=")
					n := 0
					n2, err := strconv.Atoi(strings.TrimSpace(val))
					n = n2
					if !found || err != nil {
						http.Error(w, "session.liveness_idle_timeout_overrides: invalid; use an object of backend name to whole seconds", http.StatusBadRequest)
						return
					}
					no[strings.TrimSpace(k)] = n
				}
			default:
				http.Error(w, "session.liveness_idle_timeout_overrides: invalid", http.StatusBadRequest)
				return
			}
		}
		if err := config.ValidateLivenessSettings(nw, no, []string{"claude-code", "pi-prompt", "goose"}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		window, overrides = nw, no
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
}

func TestLivenessCLISetThenGet(t *testing.T) {
	livenessStubDaemon(t)
	cases := []struct{ key, value string }{
		{"session.liveness_window_seconds", "90"},
		{"session.liveness_idle_timeout_overrides", `{"pi":300}`},
		{"session.liveness_idle_timeout_overrides", `pi=300,goose=0`},
	}
	for _, c := range cases {
		if _, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		}); err != nil {
			t.Fatalf("set %s=%s: %v", c.key, c.value, err)
		}
	}
	out, err := captureEscalationStdout(t, func() error {
		return runConfigGet(&config.Config{}, "session.liveness_idle_timeout_overrides")
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("get output %q: %v", out, err)
	}
	if got["pi"] != 300 || got["goose"] != 0 || len(got) != 2 {
		t.Errorf("overrides read back = %v", got)
	}
	out, err = captureEscalationStdout(t, func() error {
		return runConfigGet(&config.Config{}, "session.liveness_window_seconds")
	})
	if err != nil || strings.TrimSpace(out) != "90" {
		t.Errorf("window read back = %q, %v", out, err)
	}
}

func TestLivenessCLIRefusals(t *testing.T) {
	livenessStubDaemon(t)
	cases := []struct{ key, value, want string }{
		{"session.liveness_idle_timeout_overrides", `{"nosuchbackend":60}`, `unknown backend "nosuchbackend"`},
		{"session.liveness_idle_timeout_overrides", `nosuchbackend=60`, `unknown backend "nosuchbackend"`},
		{"session.liveness_idle_timeout_overrides", `pi=abc`, "invalid"},
		{"session.liveness_idle_timeout_overrides", `not a map`, "invalid"},
		{"session.liveness_window_seconds", "-1", "invalid"},
	}
	for _, c := range cases {
		_, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s=%s: want refusal containing %q, got %v", c.key, c.value, c.want, err)
		}
	}
}

func TestLivenessCLIShowFormat(t *testing.T) {
	if got := formatLivenessOverrides(nil); got != "(none)" {
		t.Errorf("empty = %q", got)
	}
	if got := formatLivenessOverrides(map[string]int{"pi": 300, "goose": 0}); got != "goose=0,pi=300" {
		t.Errorf("sorted = %q", got)
	}
}

func TestLivenessConfigureBodyObject(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(configureRequestBody("session.liveness_idle_timeout_overrides", `{"pi":300}`)), &m); err != nil {
		t.Fatal(err)
	}
	if obj, ok := m["session.liveness_idle_timeout_overrides"].(map[string]any); !ok || obj["pi"] != float64(300) {
		t.Errorf("body = %v, want an object", m)
	}
	m = nil
	if err := json.Unmarshal([]byte(configureRequestBody("session.liveness_idle_timeout_overrides", `pi=300`)), &m); err != nil {
		t.Fatal(err)
	}
	if obj, ok := m["session.liveness_idle_timeout_overrides"].(map[string]any); !ok || obj["pi"] != float64(300) {
		t.Errorf("pairs must go out as an object, got %v", m)
	}
	m = nil
	if err := json.Unmarshal([]byte(configureRequestBody("session.liveness_idle_timeout_overrides", `junk`)), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["session.liveness_idle_timeout_overrides"].(string); !ok {
		t.Errorf("unparseable text must go out as a string for the daemon to refuse, got %v", m)
	}
}
