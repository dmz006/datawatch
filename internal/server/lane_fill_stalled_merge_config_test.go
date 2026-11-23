package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// stalled_merge_minutes: the surface serves the value in effect (30 when
// unset), a PUT stores it on both config endpoints (an explicit 0 included),
// a negative, fractional or non-number value is refused with a 400 that names
// the key, and the status response carries the waiting list with each reason.

func TestStalledMergeMinutesSurfaceShowsDefault(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if got := string(laneFillSurface(t, s)["stalled_merge_minutes"]); got != "30" {
		t.Errorf("GET /api/config lane_fill.stalled_merge_minutes = %s, want 30", got)
	}
}

func TestStalledMergeMinutesPutRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	for _, tc := range []struct {
		put  int
		want string
	}{{0, "0"}, {45, "45"}, {30, "30"}} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
			"autonomous.lane_fill.stalled_merge_minutes": tc.put,
		})
		if got := string(laneFillSurface(t, s)["stalled_merge_minutes"]); got != tc.want {
			t.Errorf("after PUT %d: GET shows %s, want %s", tc.put, got, tc.want)
		}
		live := mgr.Config().LaneFill
		if live.StalledMergeMinutes == nil || live.EffectiveStalledMergeMinutes() != tc.put {
			t.Errorf("after PUT %d: live manager holds %v", tc.put, live.StalledMergeMinutes)
		}
	}
}

func TestStalledMergeMinutesPutRoundTripAutonomousConfigEndpoint(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config",
		map[string]any{"lane_fill": map[string]any{
			"mode":                  "on",
			"gpu_lanes":             2,
			"stalled_merge_minutes": 0,
		}})
	live := mgr.Config().LaneFill
	if live.StalledMergeMinutes == nil || live.EffectiveStalledMergeMinutes() != 0 {
		t.Fatalf("PUT /api/autonomous/config lane_fill.stalled_merge_minutes=0 must reach the live manager, got %v", live.StalledMergeMinutes)
	}
	if live.Mode != "on" || live.GPULanes != 2 {
		t.Fatalf("the sibling lane fill settings were lost by the round trip: %+v", live)
	}
	if got := string(laneFillSurface(t, s)["stalled_merge_minutes"]); got != "0" {
		t.Fatalf("GET /api/config after the autonomous config PUT: stalled_merge_minutes = %s, want 0", got)
	}
}

func TestStalledMergeMinutesBadValuesRefused(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	for _, bad := range []any{-1, -30, 1.5, "30", true, []int{30}} {
		b, _ := json.Marshal(map[string]any{"autonomous.lane_fill.stalled_merge_minutes": bad})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "stalled_merge_minutes") {
			t.Errorf("stalled_merge_minutes=%v: want a 400 naming the key, got %d %q", bad, rr.Code, rr.Body.String())
		}
	}
	if mgr.Config().LaneFill.StalledMergeMinutes != nil {
		t.Error("rejected PUTs must store nothing")
	}
	if got := string(laneFillSurface(t, s)["stalled_merge_minutes"]); got != "30" {
		t.Errorf("rejected PUTs changed the value in effect: %s", got)
	}
}

func TestStalledMergeMinutesBadValuesRefusedAutonomousConfigEndpoint(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	for _, bad := range []any{-1, 2.5, "30", false} {
		b, _ := json.Marshal(map[string]any{"lane_fill": map[string]any{"stalled_merge_minutes": bad}})
		rr := httptest.NewRecorder()
		s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "stalled_merge_minutes") {
			t.Errorf("stalled_merge_minutes=%v: want a 400 naming the key, got %d %q", bad, rr.Code, rr.Body.String())
		}
	}
	if mgr.Config().LaneFill.StalledMergeMinutes != nil {
		t.Error("rejected PUTs must store nothing")
	}
}

// Both endpoints refuse with the same text, which the CLI also uses.
func TestStalledMergeMinutesRefusalTextIsShared(t *testing.T) {
	want := validateLaneFillValue("stalled_merge_minutes", -1)
	if want == nil {
		t.Fatal("-1 must be refused")
	}
	if got := config.ValidateLaneFillStalledMergeMinutes(-1); got == nil || got.Error() != want.Error() {
		t.Errorf("shared check = %v, want %v", got, want)
	}
	s, _, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{"autonomous.lane_fill.stalled_merge_minutes": -1})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if !strings.Contains(rr.Body.String(), want.Error()) {
		t.Errorf("PUT /api/config text %q lacks %q", rr.Body.String(), want.Error())
	}
}

// TestStalledMergeMinutesNamedInConfigureUsage pins the router's `configure`
// usage text: the key must appear in both usage strings.
func TestStalledMergeMinutesNamedInConfigureUsage(t *testing.T) {
	src := readRepoFile(t, "internal/router/router.go")
	if count := strings.Count(src, `autonomous.lane_fill.stalled_merge_minutes`); count < 2 {
		t.Fatalf("the stalled-merge key must appear in both configure usage strings; found %d", count)
	}
}

// The status response carries the waiting list: each Automaton that holds no
// live work, with its reason text and its stable reason code.
func TestLaneFillStatusCarriesWaitingList(t *testing.T) {
	spy := newLaneSpy()
	spy.status = laneFillFixtureStatus()
	spy.status.Waiting = []autonomous.LaneFillWaitingOcc{
		{PRDID: "w1", Title: "Add export", Backend: "claude-code",
			WaitReason: "its merge has been pending for 41 minutes with no progress", ReasonCode: autonomous.LaneOccMergeStalled},
		{PRDID: "w2", Title: "Fix login", WaitReason: "every task is completed", ReasonCode: autonomous.LaneOccWaitingReviewMerge},
	}
	s := &Server{autonomousMgr: spy}
	rec := getLaneFill(s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET lane-fill: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Waiting []struct {
			PRDID      string `json:"prd_id"`
			Title      string `json:"title"`
			WaitReason string `json:"wait_reason"`
			ReasonCode string `json:"reason_code"`
		} `json:"waiting"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if len(got.Waiting) != 2 {
		t.Fatalf("waiting = %+v, want 2 entries", got.Waiting)
	}
	if got.Waiting[0].ReasonCode != "merge_stalled" || !strings.Contains(got.Waiting[0].WaitReason, "41 minutes") ||
		got.Waiting[0].Title != "Add export" {
		t.Errorf("first waiting entry = %+v", got.Waiting[0])
	}
	if got.Waiting[1].ReasonCode != "waiting_review_merge" {
		t.Errorf("second waiting entry = %+v", got.Waiting[1])
	}
}
