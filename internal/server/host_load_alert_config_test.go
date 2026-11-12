// Configuration round-trip for autonomous.host_load_alert (mode, multiple,
// minutes) on the REST config surfaces: GET/PUT /api/config and GET/PUT
// /api/autonomous/config. No process is started and no operator config read.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func hostLoadDisplay(t *testing.T, s *Server) map[string]any {
	t.Helper()
	raw := getAutonomousConfigMap(t, s)["host_load_alert"]
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode host_load_alert %q: %v", raw, err)
	}
	return m
}

func TestHostLoadAlert_GetConfigDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	m := hostLoadDisplay(t, s)
	if m["mode"] != "alert" || m["multiple"] != 1.5 || m["minutes"] != float64(10) {
		t.Fatalf("defaults = %v, want alert/1.5/10", m)
	}
}

func TestHostLoadAlert_PutConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.host_load_alert.mode":     "reap",
		"autonomous.host_load_alert.multiple": 2.5,
		"autonomous.host_load_alert.minutes":  15,
	})
	m := hostLoadDisplay(t, s)
	if m["mode"] != "reap" || m["multiple"] != 2.5 || m["minutes"] != float64(15) {
		t.Fatalf("GET after PUT = %v", m)
	}
	live := mgr.Config().HostLoadAlert
	if live.Mode != "reap" || live.Multiple != 2.5 || live.Minutes != 15 {
		t.Fatalf("live manager = %+v, want reap/2.5/15", live)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"host_load_alert:", "mode: reap", "multiple: 2.5", "minutes: 15"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, raw)
		}
	}
}

func TestHostLoadAlert_PutConfigAcceptsChatStrings(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.host_load_alert.multiple": "3",
		"autonomous.host_load_alert.minutes":  "20",
	})
	m := hostLoadDisplay(t, s)
	if m["multiple"] != float64(3) || m["minutes"] != float64(20) {
		t.Fatalf("string values not stored: %v", m)
	}
}

func TestHostLoadAlert_PutConfigRejectsBadValues(t *testing.T) {
	for name, patch := range map[string]map[string]any{
		"unknown mode":      {"autonomous.host_load_alert.mode": "kill"},
		"empty mode":        {"autonomous.host_load_alert.mode": ""},
		"negative multiple": {"autonomous.host_load_alert.multiple": -1},
		"text multiple":     {"autonomous.host_load_alert.multiple": "lots"},
		"negative minutes":  {"autonomous.host_load_alert.minutes": -5},
		"fraction minutes":  {"autonomous.host_load_alert.minutes": 1.5},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			b, _ := json.Marshal(patch)
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			if m := hostLoadDisplay(t, s); m["mode"] != "alert" {
				t.Fatalf("a rejected PUT changed the stored value: %v", m)
			}
		})
	}
}

func TestHostLoadAlert_PutAutonomousConfigRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config",
		map[string]any{"host_load_alert": map[string]any{"mode": "off", "minutes": 7}})
	live := mgr.Config().HostLoadAlert
	if live.Mode != "off" || live.Minutes != 7 {
		t.Fatalf("live manager = %+v, want off/_/7", live)
	}
	if got := s.cfg.Autonomous.HostLoadAlert; got.Mode != "off" || got.Minutes != 7 {
		t.Fatalf("s.cfg.Autonomous.HostLoadAlert = %+v, want off/_/7 (sync-back)", got)
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if !strings.Contains(rr.Body.String(), `"host_load_alert":{"mode":"off"`) {
		t.Fatalf("GET /api/autonomous/config lacks host_load_alert: %s", rr.Body.String())
	}
}

func TestHostLoadAlert_PutAutonomousConfigRejectsBadValues(t *testing.T) {
	for name, obj := range map[string]any{
		"unknown mode":    map[string]any{"mode": "kill"},
		"negative factor": map[string]any{"multiple": -2},
		"negative mins":   map[string]any{"minutes": -1},
		"unknown field":   map[string]any{"colour": "red"},
		"not an object":   "alert",
	} {
		t.Run(name, func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			b, _ := json.Marshal(map[string]any{"host_load_alert": obj})
			rr := httptest.NewRecorder()
			s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(string(b))))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			if got := mgr.Config().HostLoadAlert; got.Mode != "" {
				t.Fatalf("a rejected PUT changed the live config: %+v", got)
			}
		})
	}
}

func TestHostLoadAlert_PutAutonomousConfigNullIsAbsent(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"host_load_alert": nil})
}
