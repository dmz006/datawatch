package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// count_verifier_wait is the only boolean lane-fill setting: the surface must
// serve the effective value (unset reads true) and a PUT of false must
// survive the GET round trip as an explicit false — which is why the config
// field is a tri-state pointer rather than a plain bool.

func TestVerifierWaitSurfaceShowsTrueByDefault(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if got := string(laneFillSurface(t, s)["count_verifier_wait"]); got != "true" {
		t.Fatalf("GET /api/config lane_fill.count_verifier_wait = %s, want true (default when unset)", got)
	}
}

func TestVerifierWaitPutThenGetRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	// off -> the explicit false must show up on GET and in the live manager.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.lane_fill.count_verifier_wait": false,
	})
	if got := string(laneFillSurface(t, s)["count_verifier_wait"]); got != "false" {
		t.Fatalf("GET after PUT false: count_verifier_wait = %s, want false", got)
	}
	if got := mgr.Config().LaneFill.EffectiveCountVerifierWait(); got != false {
		t.Fatal("the live manager must resolve count_verifier_wait to false after the PUT")
	}
	if mf := mgr.Config().LaneFill.CountVerifierWait; mf == nil {
		t.Fatal("the live manager must store an explicit false, not an unset pointer")
	}
	// back on -> the explicit true must also round trip.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.lane_fill.count_verifier_wait": true,
	})
	if got := string(laneFillSurface(t, s)["count_verifier_wait"]); got != "true" {
		t.Fatalf("GET after PUT true: count_verifier_wait = %s, want true", got)
	}
	if got := mgr.Config().LaneFill.EffectiveCountVerifierWait(); got != true {
		t.Fatal("the live manager must resolve count_verifier_wait to true after the PUT")
	}
}

func TestVerifierWaitPutRoundTripAutonomousConfigEndpoint(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config",
		map[string]any{"lane_fill": map[string]any{
			"mode":                "on",
			"gpu_lanes":           2,
			"claude_lanes":        3,
			"count_verifier_wait": false,
		}})
	if got := mgr.Config().LaneFill.EffectiveCountVerifierWait(); got != false {
		t.Fatal("PUT /api/autonomous/config lane_fill.count_verifier_wait=false must reach the live manager")
	}
	live := mgr.Config().LaneFill
	if live.Mode != "on" || live.GPULanes != 2 || live.ClaudeLanes != 3 {
		t.Fatalf("the sibling lane fill settings were lost by the round trip: %+v", live)
	}
	// the same value must be served by GET /api/config autonomous.lane_fill
	// (the manager->config sync runs after the merge).
	if got := string(laneFillSurface(t, s)["count_verifier_wait"]); got != "false" {
		t.Fatalf("GET /api/config after the autonomous config PUT: count_verifier_wait = %s, want false", got)
	}
}

func TestVerifierWaitRejectsNonBoolValue(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	before := string(laneFillSurface(t, s)["count_verifier_wait"])
	for _, bad := range []any{"yes", 0, 1, 1.0, []bool{true}, "true"} {
		b, _ := json.Marshal(map[string]any{"autonomous.lane_fill.count_verifier_wait": bad})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%v: a non-boolean count_verifier_wait must be 400, got %d: %s", bad, rr.Code, rr.Body.String())
		}
	}
	if got := string(laneFillSurface(t, s)["count_verifier_wait"]); got != before {
		t.Fatalf("rejected PUTs changed count_verifier_wait: %s -> %s", before, got)
	}
	if mf := mgr.Config().LaneFill.CountVerifierWait; mf != nil {
		t.Fatalf("rejected PUTs must not store a value, got %v", *mf)
	}
}

func TestVerifierWaitRejectsNonBoolValueAutonomousConfigEndpoint(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{"lane_fill": map[string]any{"count_verifier_wait": "yes"}})
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("a non-boolean count_verifier_wait must be 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestVerifierWaitNamedInConfigureUsage pins the router's `configure` usage
// text: the key must appear twice, once in each of the two usage strings
// (mirrors the judge size-threshold parity test).
func TestVerifierWaitNamedInConfigureUsage(t *testing.T) {
	src := readRepoFile(t, "internal/router/router.go")
	count := strings.Count(src, `autonomous.lane_fill.count_verifier_wait`)
	if count < 2 {
		t.Fatalf("the lane fill verifier-wait key must appear in both configure usage strings; found %d", count)
	}
}
