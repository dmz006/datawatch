// autonomous.auto_fix_retries — the documented config surface, proved in
// one place: REST round-trip (PUT /api/config then GET /api/config, plus
// the live autonomous-manager propagation the daemon uses for its
// config read), refusal of negative and non-number values, and the YAML
// key → config load round-trip.
//
// The comm channel's `configure` (internal/router/compute.go), the MCP
// config_set (internal/mcp/bl220_mcp_tools.go) and the web app's
// "Auto-fix retries" input (internal/server/web/app.js) all send this
// same key through this same PUT /api/config route, so covering the REST
// write covers those write surfaces; the YAML case covers the
// daemon-start path.
//
// Fakes only: config lives under t.TempDir(), the live autonomous manager
// is the embedded AutonomousAPI fake, nothing outside the process is
// touched.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// putAutoFixRetriesPatch sends one autonomous.auto_fix_retries key to
// PUT /api/config against the given bare-Server process instance.
func putAutoFixRetriesPatch(t *testing.T, s *Server, v interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"autonomous.auto_fix_retries": v})
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	return rr
}

// forwardedAutoFixRetries reports what the live autonomous manager last
// received under auto_fix_retries (its autonomous config read).
func forwardedAutoFixRetries(t *testing.T, spy *autonomousConfigPropagationSpy) (int, bool) {
	t.Helper()
	if spy.calls == 0 {
		return 0, false
	}
	var forwarded map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &forwarded); err != nil {
		t.Fatalf("decode forwarded config: %v (%s)", err, spy.lastSetConfig)
	}
	v, ok := forwarded["auto_fix_retries"]
	if !ok {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		t.Fatalf("decode forwarded auto_fix_retries: %v (%s)", err, v)
	}
	return n, true
}

// TestAutoFixRetriesRestRoundTrip — a whole number written through
// PUT /api/config sticks in the loaded config, is persisted to the YAML,
// reads back under autonomous.auto_fix_retries on GET /api/config, and is
// propagated to the live autonomous manager.
func TestAutoFixRetriesRestRoundTrip(t *testing.T) {
	cfg := &config.Config{}
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}

	rr := putAutoFixRetriesPatch(t, s, 3)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := cfg.Autonomous.AutoFixRetries; got != 3 {
		t.Fatalf("stored AutoFixRetries = %d, want 3", got)
	}
	// Persisted to the YAML the way a restart would reload it.
	disk, err := os.ReadFile(s.cfgPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(disk), "auto_fix_retries: 3") {
		t.Errorf("saved config.yaml does not contain \"auto_fix_retries: 3\":\n%s", disk)
	}
	// GET /api/config answers the written value under autonomous.auto_fix_retries.
	rr = httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			AutoFixRetries int `json:"auto_fix_retries"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.AutoFixRetries != 3 {
		t.Errorf("GET auto_fix_retries = %d, want 3", got.Autonomous.AutoFixRetries)
	}
	// The live autonomous manager received exactly the new value.
	if n, ok := forwardedAutoFixRetries(t, spy); !ok || n != 3 {
		t.Errorf("forwarded auto_fix_retries = %d (present %v), want 3", n, ok)
	}
}

// TestAutoFixRetriesRejectsBadValues — a negative whole number and a
// non-number value are not written: the value stays at its previous value,
// the response is still ok, and the live manager never sees the bad value.
// A whole number sent as a JSON string is refused by the same rule
// (this surface takes JSON numbers; the comm channel and the web app both
// send numbers). Separately, a hand-written non-whole YAML value fails the
// load instead of shipping a budget that is not a whole number.
func TestAutoFixRetriesRejectsBadValues(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.AutoFixRetries = 1 // the shipped default, the value to keep
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml"), autonomousMgr: spy}

	for name, v := range map[string]interface{}{
		"negative":  -2,
		"json_str":  "3",
		"not_a_num": "abc",
	} {
		before := cfg.Autonomous.AutoFixRetries
		rr := putAutoFixRetriesPatch(t, s, v)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: PUT: want 200 (refused value, silent no-op), got %d: %s",
				name, rr.Code, rr.Body.String())
		}
		if got := cfg.Autonomous.AutoFixRetries; got != before {
			t.Errorf("%s: stored AutoFixRetries = %d, want %d (refused value must not stick)",
				name, got, before)
		}
		if n, ok := forwardedAutoFixRetries(t, spy); ok && n != before {
			t.Errorf("%s: live manager saw auto_fix_retries = %d, want %d", name, n, before)
		}
	}

	// A hand-written non-whole YAML value fails the load.
	badPath := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(badPath, []byte("autonomous:\n  auto_fix_retries: not-a-number\n"), 0o600); err != nil {
		t.Fatalf("write bad yaml: %v", err)
	}
	if _, err := config.Load(badPath); err == nil {
		t.Errorf("config.Load of a non-whole auto_fix_retries must fail")
	}
}

// TestAutoFixRetriesYAMLRoundTrip — the key as written in config.yaml
// loads into the config and survives a save/load cycle, and the shipped
// default (1) is where an unset key resolves at the daemon.
func TestAutoFixRetriesYAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  auto_fix_retries: 5\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if got := cfg.Autonomous.AutoFixRetries; got != 5 {
		t.Fatalf("loaded AutoFixRetries = %d, want 5", got)
	}
	// Save and reload: the written value round-trips through the YAML.
	savePath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(cfg, savePath); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	again, err := config.Load(savePath)
	if err != nil {
		t.Fatalf("config.Load after Save: %v", err)
	}
	if got := again.Autonomous.AutoFixRetries; got != 5 {
		t.Errorf("reloaded AutoFixRetries = %d, want 5", got)
	}
	// The shipped default for a file that never sets the key (internal/config
	// DefaultConfig) is 1 — one content auto-fix retry.
	if got := config.DefaultConfig().Autonomous.AutoFixRetries; got != 1 {
		t.Errorf("DefaultConfig AutoFixRetries = %d, want 1", got)
	}
}
