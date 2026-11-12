package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// host_down_after_minutes: the surface serves the value in effect (10 when
// unset), a PUT stores it (an explicit 0 included), and a negative or
// non-integer value is refused with a 400 that names the key.

func TestLaneFillHostDownAfterMinutesSurfaceShowsDefault(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	block := laneFillSurface(t, s)
	if got := string(block["host_down_after_minutes"]); got != "10" {
		t.Errorf("GET /api/config lane_fill.host_down_after_minutes = %s, want 10", got)
	}
}

func TestLaneFillHostDownAfterMinutesPutRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	for _, tc := range []struct {
		put  int
		want string
	}{{0, "0"}, {25, "25"}} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
			"autonomous.lane_fill.host_down_after_minutes": tc.put,
		})
		block := laneFillSurface(t, s)
		if got := string(block["host_down_after_minutes"]); got != tc.want {
			t.Errorf("after PUT %d: GET shows %s, want %s", tc.put, got, tc.want)
		}
		live := mgr.Config().LaneFill
		if live.HostDownAfterMinutes == nil || live.EffectiveHostDownAfterMinutes() != tc.put {
			t.Errorf("after PUT %d: live manager holds %v", tc.put, live.HostDownAfterMinutes)
		}
	}
}

func TestLaneFillHostDownAfterMinutesBadValuesRefused(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	for _, bad := range []any{-1, 1.5, "10", true} {
		b, _ := json.Marshal(map[string]any{"autonomous.lane_fill.host_down_after_minutes": bad})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "host_down_after_minutes") {
			t.Errorf("host_down_after_minutes=%v: want a 400 naming the key, got %d %q", bad, rr.Code, rr.Body.String())
		}
	}
	if mgr.Config().LaneFill.HostDownAfterMinutes != nil {
		t.Error("rejected PUTs must store nothing")
	}
}
