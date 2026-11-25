package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// GET /api/supervisor-loop/wake shows the designated session, the current
// target and why.
func TestSupervisorWakeStatusShowsTargetAndReason(t *testing.T) {
	s, _ := newLoopServer(t)
	s.cfg.Autonomous.SupervisorLoop = config.SupervisorLoopConfig{supervisorloop.KeyWakeSession: "sup1"}
	read := func() map[string]any {
		rr := httptest.NewRecorder()
		s.handleSupervisorLoopWake(rr, httptest.NewRequest(http.MethodGet, "/api/supervisor-loop/wake", nil))
		var m map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	// With no resolver wired the designation is still reported.
	if m := read(); m["session"] != "sup1" || m["target"] != "" {
		t.Fatalf("unwired: %v", m)
	}
	s.SetSupervisorWakeTarget(func() supervisorloop.WakeTarget {
		return supervisorloop.WakeTarget{Session: "sup1", Target: "host-sup1", Code: supervisorloop.WakeTargetOK, Reason: "designated and running"}
	})
	m := read()
	if m["session"] != "sup1" || m["target"] != "host-sup1" || m["target_code"] != "ok" || m["target_reason"] != "designated and running" {
		t.Fatalf("wired: %v", m)
	}
	if _, ok := m["event_count"]; !ok {
		t.Fatalf("the wake record fields are gone: %v", m)
	}
}

// The designated session is one id without spaces.
func TestSupervisorWakeSessionSettingValidated(t *testing.T) {
	for _, bad := range []any{"two ids", "a\nb", 7, string(make([]byte, 200))} {
		if err := validateSupervisorLoopPatch(config.SupervisorLoopConfig{}, map[string]interface{}{"autonomous.supervisor_loop.wake.session": bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"", "host-1a2b", "  1a2b  "} {
		if err := validateSupervisorLoopPatch(config.SupervisorLoopConfig{}, map[string]interface{}{"autonomous.supervisor_loop.wake.session": good}); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
}
