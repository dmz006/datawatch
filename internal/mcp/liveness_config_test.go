package mcp

import (
	"context"
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

// livenessStubDaemon stands in for PUT/GET /api/config: it applies the same
// ValidateLivenessSettings rule the daemon does and keeps the session block.
func livenessStubDaemon(t *testing.T) *Server {
	t.Helper()
	var mu sync.Mutex
	window := 0
	overrides := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			var patch map[string]any
			if err := json.Unmarshal(raw, &patch); err != nil {
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
				obj, isObj := v.(map[string]any)
				if !isObj {
					http.Error(w, "session.liveness_idle_timeout_overrides: invalid; use an object", http.StatusBadRequest)
					return
				}
				no = map[string]int{}
				for k, raw := range obj {
					f, _ := raw.(float64)
					no[k] = int(f)
				}
			}
			if err := config.ValidateLivenessSettings(nw, no, []string{"claude-code", "pi-prompt", "goose"}); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			window, overrides = nw, no
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{
				"liveness_window_seconds":         window,
				"liveness_idle_timeout_overrides": overrides,
			}})
		}
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
}

func TestLivenessMCPRoundTrip(t *testing.T) {
	s := livenessStubDaemon(t)
	ctx := context.Background()

	for _, c := range [][2]string{
		{"session.liveness_window_seconds", "120"},
		{"session.liveness_idle_timeout_overrides", `{"pi":300,"goose":0}`},
	} {
		res, err := s.handleConfigSet(ctx, callReq(map[string]any{"key": c[0], "value": c[1]}))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("set %s: err=%v res=%+v", c[0], err, res)
		}
	}
	res, err := s.handleGetConfig(ctx, callReq(map[string]any{"section": "session"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("get: err=%v res=%+v", err, res)
	}
	var got struct {
		Window    int            `json:"liveness_window_seconds"`
		Overrides map[string]int `json:"liveness_idle_timeout_overrides"`
	}
	if err := json.Unmarshal([]byte(automationText(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Window != 120 || got.Overrides["pi"] != 300 || got.Overrides["goose"] != 0 || len(got.Overrides) != 2 {
		t.Errorf("read back %+v", got)
	}
}

func TestLivenessMCPRefusals(t *testing.T) {
	s := livenessStubDaemon(t)
	ctx := context.Background()
	cases := []struct{ key, value, want string }{
		{"session.liveness_idle_timeout_overrides", `{"nosuchbackend":60}`, `unknown backend "nosuchbackend"`},
		{"session.liveness_idle_timeout_overrides", `{"pi":-5}`, "invalid"},
		{"session.liveness_window_seconds", "-1", "invalid"},
	}
	for _, c := range cases {
		res, err := s.handleConfigSet(ctx, callReq(map[string]any{"key": c.key, "value": c.value}))
		if err != nil || res == nil || !res.IsError {
			t.Errorf("%s=%s must be a tool error: err=%v res=%+v", c.key, c.value, err, res)
			continue
		}
		if text := automationText(t, res); !strings.Contains(text, c.want) {
			t.Errorf("%s=%s: refusal text %q lacks %q", c.key, c.value, text, c.want)
		}
	}
	// The refusal must not have changed anything.
	res, _ := s.handleGetConfig(ctx, callReq(map[string]any{"section": "session"}))
	if text := automationText(t, res); strings.Contains(text, "nosuchbackend") {
		t.Errorf("refused value was stored: %s", text)
	}
}

func TestLivenessMCPDescriptionsListKeys(t *testing.T) {
	d := (&Server{}).toolConfigSet().Description
	for _, k := range []string{"session.liveness_window_seconds", "session.liveness_idle_timeout_overrides"} {
		if !strings.Contains(d, k) {
			t.Errorf("config_set description lacks %s", k)
		}
	}
}

func TestLivenessMCPPairsAndBadText(t *testing.T) {
	s := livenessStubDaemon(t)
	ctx := context.Background()
	res, err := s.handleConfigSet(ctx, callReq(map[string]any{"key": "session.liveness_idle_timeout_overrides", "value": "pi=300,goose=0"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("pairs form refused: err=%v res=%+v", err, res)
	}
	res, _ = s.handleConfigSet(ctx, callReq(map[string]any{"key": "session.liveness_idle_timeout_overrides", "value": "not a map"}))
	if res == nil || !res.IsError || !strings.Contains(automationText(t, res), "backend=seconds") {
		t.Errorf("bad text must be refused with a usage message: %+v", res)
	}
}
