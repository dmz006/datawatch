package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// mergeShadowSurface reads the autonomous.merge_shadow block out of
// GET /api/config.
func mergeShadowSurface(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	block, ok := getAutonomousConfigMap(t, s)["merge_shadow"]
	if !ok {
		var keys []string
		for k := range getAutonomousConfigMap(t, s) {
			keys = append(keys, k)
		}
		t.Fatalf(`GET autonomous map: no "merge_shadow" block (keys: %v)`, keys)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(block, &out); err != nil {
		t.Fatalf(`unmarshal merge_shadow block: %v (block %s)`, err, block)
	}
	return out
}

func putMergeShadow(t *testing.T, s *Server, key string, value interface{}) {
	t.Helper()
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]interface{}{key: value})
}

// TestMergeShadowConfigPutGetRoundTrip is the acceptance: each of the three
// autonomous.merge_shadow.* keys round-trips through PUT /api/config and back
// out through GET /api/config.
func TestMergeShadowConfigPutGetRoundTrip(t *testing.T) {
	s, _, _ := newActivateServer(t, false)

	cases := []struct {
		key   string // autonomous.merge_shadow.<field>
		value interface{}
	}{
		{"autonomous.merge_shadow.enabled", false},
		{"autonomous.merge_shadow.gate_timeout", "45m"},
		{"autonomous.merge_shadow.plan_keep", 7},
	}
	for _, c := range cases {
		putMergeShadow(t, s, c.key, c.value)
		surface := mergeShadowSurface(t, s)
		switch c.value {
		case false:
			if surface["enabled"] != false {
				t.Errorf("%s round trip: enabled = %v, want false", c.key, surface["enabled"])
			}
		case "45m":
			if surface["gate_timeout"] != "45m0s" {
				t.Errorf("%s round trip: gate_timeout = %v, want \"45m0s\"", c.key, surface["gate_timeout"])
			}
		case 7:
			if surface["plan_keep"] != float64(7) {
				t.Errorf("%s round trip: plan_keep = %v, want 7", c.key, surface["plan_keep"])
			}
		default:
			t.Fatalf("test case table: unhandled value %v", c.value)
		}
	}

	// The stored config block carries the same values, pointer-set.
	ms := s.cfg.Autonomous.MergeShadow
	if ms.Enabled == nil || *ms.Enabled != false {
		t.Errorf("stored MergeShadow.Enabled = %v, want pointer to false", ms.Enabled)
	}
	if ms.GateTimeout == nil || *ms.GateTimeout != "45m" {
		t.Errorf("stored MergeShadow.GateTimeout = %v, want pointer to \"45m\"", ms.GateTimeout)
	}
	if ms.PlanKeep == nil || *ms.PlanKeep != 7 {
		t.Errorf("stored MergeShadow.PlanKeep = %v, want pointer to 7", ms.PlanKeep)
	}
}

// TestMergeShadowConfigDefaults is the GET side before any PUT: the block is
// there and reads the defaults (enabled on, 20-minute gate timeout, 200 plans
// kept per project).
func TestMergeShadowConfigDefaults(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	surface := mergeShadowSurface(t, s)
	want := map[string]interface{}{
		"enabled":      true,
		"gate_timeout": config.DefaultMergeShadowGateTimeout.String(),
		"plan_keep":    float64(config.DefaultMergeShadowPlanKeep),
	}
	for k, v := range want {
		if surface[k] != v {
			t.Errorf("default %s = %v, want %v", k, surface[k], v)
		}
	}
}

// TestMergeShadowConfigRejectsBadValues is the validation: an unparseable or
// non-positive gate_timeout, a non-positive or non-whole plan_keep, a
// non-boolean enabled and an unknown field each reject the PUT, and none of
// them displaces the earlier good values.
func TestMergeShadowConfigRejectsBadValues(t *testing.T) {
	s, _, _ := newActivateServer(t, false)

	bad := []struct {
		key   string
		value interface{}
	}{
		{"autonomous.merge_shadow.gate_timeout", "not-a-duration"},
		{"autonomous.merge_shadow.gate_timeout", "0s"},
		{"autonomous.merge_shadow.gate_timeout", "-5m"},
		{"autonomous.merge_shadow.gate_timeout", 900},
		{"autonomous.merge_shadow.plan_keep", 0},
		{"autonomous.merge_shadow.plan_keep", -3},
		{"autonomous.merge_shadow.plan_keep", 2.5},
		{"autonomous.merge_shadow.enabled", "on"},
		{"autonomous.merge_shadow.enabled", 1},
		{"autonomous.merge_shadow.release_mode", "shadow"},
	}

	// A good pair first, so a wrongly accepted bad value would be visible.
	putMergeShadow(t, s, "autonomous.merge_shadow.gate_timeout", "30m")
	putMergeShadow(t, s, "autonomous.merge_shadow.plan_keep", 11)

	for _, c := range bad {
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]interface{}{c.key: c.value})
	}

	// The rejects never applied: the stored block still holds the good pair.
	ms := s.cfg.Autonomous.MergeShadow
	if ms.GateTimeout == nil || *ms.GateTimeout != "30m" {
		t.Errorf("gate_timeout after refusals = %v, want the earlier \"30m\"", ms.GateTimeout)
	}
	if ms.PlanKeep == nil || *ms.PlanKeep != 11 {
		t.Errorf("plan_keep after refusals = %v, want the earlier 11", ms.PlanKeep)
	}
	surface := mergeShadowSurface(t, s)
	if got := surface["gate_timeout"]; got != "30m0s" {
		t.Errorf("surface gate_timeout = %v after refusals, want \"30m0s\"", got)
	}
	if got := surface["plan_keep"]; got != float64(11) {
		t.Errorf("surface plan_keep = %v after refusals, want 11", got)
	}
}

// TestMergeShadowConfigPutRejectsWholePatch proves one bad value in a multi-key
// PUT rejects the whole patch: the good sibling key does not land either.
func TestMergeShadowConfigPutRejectsWholePatch(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	body := `{"autonomous.merge_shadow.plan_keep":5,"autonomous.merge_shadow.gate_timeout":"xyz"}`
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader([]byte(body))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("mixed PUT status = %d (body %s), want 400", rr.Code, rr.Body.String())
	}
	if v := s.cfg.Autonomous.MergeShadow.PlanKeep; v != nil {
		t.Errorf("plan_keep = %v after a rejected multi-key PUT, want unapplied (nil)", v)
	}
}
