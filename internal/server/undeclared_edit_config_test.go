// Undeclared-edit enforcement mode — Configuration Accessibility round-trip:
// fresh GET /api/config must show autonomous.undeclared_edit_mode = "warn",
// PUT /api/config then GET /api/config must return the new mode, and an
// invalid value must reject the whole PUT with a 400.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigRoundTrip_UndeclaredEditMode(t *testing.T) {
	s := &Server{
		cfg:     &config.Config{},
		cfgPath: t.TempDir() + "/config.yaml",
	}
	get := func() string {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d", rr.Code)
		}
		var got struct {
			Autonomous map[string]json.RawMessage `json:"autonomous"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		raw, ok := got.Autonomous["undeclared_edit_mode"]
		if !ok {
			t.Fatalf("GET /api/config autonomous map lacks undeclared_edit_mode")
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decode undeclared_edit_mode: %v", err)
		}
		return v
	}
	put := func(v interface{}) int {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"autonomous.undeclared_edit_mode": v})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		return rr.Code
	}

	// Fresh (unset) value must render the "warn" default, not an empty field.
	if got := get(); got != "warn" {
		t.Fatalf("fresh GET undeclared_edit_mode = %q, want \"warn\"", got)
	}

	// PUT+GET round-trip for each valid mode, stored lowercase.
	for _, mode := range []string{"off", "block", "BLOCK", "warn"} {
		if code := put(mode); code != http.StatusOK {
			t.Fatalf("PUT %q: want 200, got %d", mode, code)
		}
		want := mode
		if mode == "BLOCK" {
			want = "block"
		}
		if got := get(); got != want {
			t.Fatalf("GET undeclared_edit_mode = %q after PUT %q, want %q", got, mode, want)
		}
	}

	// Invalid values must reject the whole PUT with 400 and not mutate the
	// stored value.
	for _, bad := range []interface{}{"", "yes", "OFF_BLOCK", true, 3} {
		if code := put(bad); code != http.StatusBadRequest {
			t.Fatalf("PUT invalid %v: want 400, got %d", bad, code)
		}
	}
	if got := get(); got != "warn" {
		t.Fatalf("GET undeclared_edit_mode = %q after failed PUTs, want prior valid value \"warn\"", got)
	}
}

// TestUndeclaredEditMode_RealManager runs both REST config surfaces against
// the real autonomous manager: a bad value must reject the whole PUT with
// 400 (including PUT /api/autonomous/config) and leave the live config alone.
func TestUndeclaredEditMode_RealManager(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)

	if got := string(getAutonomousConfigMap(t, s)["undeclared_edit_mode"]); got != `"warn"` {
		t.Fatalf("GET /api/config undeclared_edit_mode default = %q, want \"warn\"", got)
	}

	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"undeclared_edit_mode": "block"})
	if got := mgr.Config().UndeclaredEditMode; got != "block" {
		t.Fatalf("live manager UndeclaredEditMode = %q, want block", got)
	}
	if got := s.cfg.Autonomous.UndeclaredEditMode; got != "block" {
		t.Fatalf("s.cfg.Autonomous.UndeclaredEditMode = %q, want block (GH#180 sync-back)", got)
	}

	for _, bad := range []string{"maybe", "", "yes"} {
		rr := httptest.NewRecorder()
		s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config",
			strings.NewReader(`{"undeclared_edit_mode":`+fmt.Sprintf("%q", bad)+`,"max_concurrent_tasks":7}`)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT /api/autonomous/config undeclared_edit_mode=%q: want 400, got %d body=%s", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"off", "warn" or "block"`) {
			t.Errorf("rejection lacks accepted values: %s", rr.Body.String())
		}
		if got := mgr.Config().UndeclaredEditMode; got != "block" {
			t.Errorf("manager mode = %q after rejected PUT %q, want unchanged block", got, bad)
		}
		if got := mgr.Config().MaxConcurrentTasks; got == 7 {
			t.Errorf("rejected PUT %q still applied sibling field max_concurrent_tasks", bad)
		}
	}
}
