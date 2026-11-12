package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// laneSpy is the AutonomousAPI double for the lane fill routes. It records the
// status limit, the lane preference setter and the create and delete calls.
type laneSpy struct {
	fakeOrchAutonomous
	known       map[string]bool
	status      autonomous.LaneFillStatus
	gotLimit    int
	statusCalls int
	setID       string
	setPref     string
	setCalls    int
	failSet     bool
	createCall  int
	deleted     []string
}

func newLaneSpy(ids ...string) *laneSpy {
	l := &laneSpy{known: map[string]bool{}}
	for _, id := range ids {
		l.known[id] = true
	}
	return l
}

func (l *laneSpy) LaneFillStatus(limit int) autonomous.LaneFillStatus {
	l.statusCalls++
	l.gotLimit = limit
	return l.status
}

func (l *laneSpy) SetPRDLanePreference(id, pref string) (any, error) {
	l.setCalls++
	norm, ok := autonomous.NormalizeLanePreference(pref)
	if !ok {
		return nil, errors.New("unknown lane preference")
	}
	if !l.known[id] {
		return nil, errors.New("prd not found")
	}
	if l.failSet {
		return nil, errors.New("store unavailable")
	}
	l.setID, l.setPref = id, norm
	return map[string]any{"id": id, "lane_preference": norm}, nil
}

func (l *laneSpy) GetPRD(id string) (any, bool) {
	if !l.known[id] {
		return nil, false
	}
	return map[string]any{"id": id, "lane_preference": l.setPref}, true
}

func (l *laneSpy) CreatePRD(string, string, string, string, string) (any, error) {
	l.createCall++
	l.known["new1"] = true
	return map[string]any{"id": "new1"}, nil
}

func (l *laneSpy) DeletePRD(id string) error {
	l.deleted = append(l.deleted, id)
	return nil
}

func laneFillFixtureStatus() autonomous.LaneFillStatus {
	return autonomous.LaneFillStatus{
		Mode:       "shadow",
		LastPollAt: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC),
		Lanes: []autonomous.LaneFillLaneStatus{
			{ID: "gpu-1", Type: "gpu", State: "idle", EligibleWaiting: 2, Blocker: "no ready Automaton"},
		},
		QueuedEligible: autonomous.LaneFillQueued{GPU: 2, Claude: 1},
		Evidence: autonomous.LaneFillEvidence{
			CheckedAt:         time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC),
			GPUActiveSessions: 0,
			LoadedModels:      []autonomous.LoadedModel{{Host: "gpu-box", Model: "qwen3.8:27b"}},
		},
		Decisions: []autonomous.LaneFillDecision{
			{Mode: "shadow", LaneID: "gpu-1", LaneType: "gpu", Action: "start", PRDID: "abc", Title: "Parser", Reason: "lane idle", RepeatCount: 1},
		},
		Alerts: []autonomous.LaneFillAlert{{LaneID: "gpu-1", Work: "Parser"}},
	}
}

func getLaneFill(s *Server, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousLaneFill(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/lane-fill"+query, nil))
	return rec
}

func TestLaneFillStatusShape(t *testing.T) {
	spy := newLaneSpy()
	spy.status = laneFillFixtureStatus()
	s := &Server{autonomousMgr: spy}
	rec := getLaneFill(s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET lane-fill: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	for _, key := range []string{"mode", "last_poll_at", "lanes", "queued_eligible", "evidence", "decisions", "alerts"} {
		if _, ok := got[key]; !ok {
			t.Errorf("status lacks %q: %s", key, rec.Body.String())
		}
	}
	var lanes []map[string]json.RawMessage
	if err := json.Unmarshal(got["lanes"], &lanes); err != nil || len(lanes) != 1 {
		t.Fatalf("lanes: %v %s", err, got["lanes"])
	}
	for _, key := range []string{"id", "type", "state", "occupant", "blocker", "eligible_waiting"} {
		if _, ok := lanes[0][key]; !ok {
			t.Errorf("lane lacks %q", key)
		}
	}
	var decisions []map[string]json.RawMessage
	if err := json.Unmarshal(got["decisions"], &decisions); err != nil || len(decisions) != 1 {
		t.Fatalf("decisions: %v %s", err, got["decisions"])
	}
	for _, key := range []string{"at", "mode", "lane_id", "lane_type", "action", "prd_id", "title", "reason", "blocker", "executed", "error", "repeat_count", "last_seen_at"} {
		if _, ok := decisions[0][key]; !ok {
			t.Errorf("decision lacks %q", key)
		}
	}
	var mode string
	if err := json.Unmarshal(got["mode"], &mode); err != nil || mode != "shadow" {
		t.Fatalf("mode = %q (%v), want shadow", mode, err)
	}
}

func TestLaneFillLimitHandling(t *testing.T) {
	cases := map[string]int{
		"":                   50,
		"?limit=7":           7,
		"?limit=1000":        1000,
		"?limit=1&x=ignored": 1,
	}
	for query, want := range cases {
		spy := newLaneSpy()
		s := &Server{autonomousMgr: spy}
		if rec := getLaneFill(s, query); rec.Code != http.StatusOK || spy.gotLimit != want {
			t.Errorf("query %q: code=%d limit=%d, want 200 and limit %d", query, rec.Code, spy.gotLimit, want)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=-3", "?limit=abc", "?limit=1001", "?limit=", "?limit=2.5"} {
		spy := newLaneSpy()
		s := &Server{autonomousMgr: spy}
		rec := getLaneFill(s, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: want 400, got %d", query, rec.Code)
		}
		if spy.statusCalls != 0 {
			t.Errorf("query %q: an invalid limit must not reach the manager", query)
		}
	}
}

func TestLaneFillMethodGuard(t *testing.T) {
	spy := newLaneSpy()
	s := &Server{autonomousMgr: spy}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleAutonomousLaneFill(rec, httptest.NewRequest(method, "/api/autonomous/lane-fill", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	if spy.statusCalls != 0 {
		t.Errorf("refused methods must not reach the manager, got %d calls", spy.statusCalls)
	}
}

func TestLaneFillRESTFederationDenial(t *testing.T) {
	s := &Server{autonomousMgr: newLaneSpy()}
	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/lane-fill", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleAutonomousLaneFill(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/lane-fill", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleAutonomousLaneFill(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the read capability: want 200, got %d", rec.Code)
	}
}

func postLanePreference(s *Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestLaneFillSetPreferenceHappyPath(t *testing.T) {
	for _, pref := range []string{"gpu", "claude", " GPU "} {
		spy := newLaneSpy("abc")
		s := &Server{autonomousMgr: spy}
		rec := postLanePreference(s, "/api/autonomous/prds/abc/set_lane_preference", `{"lane_preference":"`+pref+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("preference %q: want 200, got %d: %s", pref, rec.Code, rec.Body.String())
		}
		want, _ := autonomous.NormalizeLanePreference(pref)
		if spy.setID != "abc" || spy.setPref != want {
			t.Fatalf("preference %q not applied: id=%q pref=%q", pref, spy.setID, spy.setPref)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got["lane_preference"] != want {
			t.Fatalf("reply must carry the preference: %v %s", err, rec.Body.String())
		}
	}
}

func TestLaneFillSetPreferenceAnyClears(t *testing.T) {
	spy := newLaneSpy("abc")
	spy.setPref = "gpu"
	s := &Server{autonomousMgr: spy}
	rec := postLanePreference(s, "/api/autonomous/prds/abc/set_lane_preference", `{"lane_preference":"any"}`)
	if rec.Code != http.StatusOK || spy.setCalls != 1 || spy.setPref != "" {
		t.Fatalf("any must clear the preference: code=%d calls=%d pref=%q", rec.Code, spy.setCalls, spy.setPref)
	}
}

func TestLaneFillSetPreferenceUnknownValue(t *testing.T) {
	spy := newLaneSpy("abc")
	s := &Server{autonomousMgr: spy}
	rec := postLanePreference(s, "/api/autonomous/prds/abc/set_lane_preference", `{"lane_preference":"fast"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown value must be 400, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, word := range []string{"gpu", "claude", "any"} {
		if !strings.Contains(rec.Body.String(), word) {
			t.Fatalf("error must name %q: %s", word, rec.Body.String())
		}
	}
	if spy.setCalls != 0 {
		t.Fatalf("an unknown value must not reach the manager, got %d calls", spy.setCalls)
	}
	if rec := postLanePreference(s, "/api/autonomous/prds/abc/set_lane_preference", `nope`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json must be 400, got %d", rec.Code)
	}
}

func TestLaneFillSetPreferenceUnknownID(t *testing.T) {
	spy := newLaneSpy("abc")
	s := &Server{autonomousMgr: spy}
	rec := postLanePreference(s, "/api/autonomous/prds/missing/set_lane_preference", `{"lane_preference":"gpu"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id must be 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLaneFillSetPreferenceMethodGuard(t *testing.T) {
	s := &Server{autonomousMgr: newLaneSpy("abc")}
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/abc/set_lane_preference", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET must be 405, got %d", rec.Code)
	}
}

func TestLaneFillSetPreferenceFederationDenial(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	spy := newLaneSpy("abc")
	s.autonomousMgr = spy
	capTest(t, s,
		http.MethodPost, "/api/autonomous/prds/abc/set_lane_preference", []byte(`{"lane_preference":"gpu"}`),
		s.handleAutonomousPRDs,
		http.StatusOK,        // admin: unrestricted
		http.StatusOK,        // full-control: holds the autonomous write capability
		http.StatusForbidden, // read-only peer: lacks it
	)
	if spy.setCalls != 2 {
		t.Fatalf("only the admin and full-control calls may reach the manager, got %d", spy.setCalls)
	}
}

func TestLaneFillCreateWithPreference(t *testing.T) {
	spy := newLaneSpy()
	s := &Server{autonomousMgr: spy}
	rec := postLanePreference(s, "/api/autonomous/prds", `{"spec":"fix the leak","project_dir":"/srv/x","lane_preference":"claude"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create with preference: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if spy.createCall != 1 || spy.setID != "new1" || spy.setPref != "claude" {
		t.Fatalf("preference not applied to the new Automaton: creates=%d id=%q pref=%q", spy.createCall, spy.setID, spy.setPref)
	}
}

func TestLaneFillCreateWithoutPreferenceLeavesItUnset(t *testing.T) {
	spy := newLaneSpy()
	s := &Server{autonomousMgr: spy}
	rec := postLanePreference(s, "/api/autonomous/prds", `{"spec":"add paging","project_dir":"/srv/x"}`)
	if rec.Code != http.StatusOK || spy.createCall != 1 || spy.setCalls != 0 {
		t.Fatalf("code=%d creates=%d preference calls=%d", rec.Code, spy.createCall, spy.setCalls)
	}
}

func TestLaneFillCreateInvalidPreferenceCreatesNothing(t *testing.T) {
	spy := newLaneSpy()
	s := &Server{autonomousMgr: spy}
	rec := postLanePreference(s, "/api/autonomous/prds", `{"spec":"x","project_dir":"/srv/x","lane_preference":"fast"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown preference on create must be 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "gpu, claude or any") {
		t.Fatalf("error must name the valid preferences: %s", rec.Body.String())
	}
	if spy.createCall != 0 {
		t.Fatalf("nothing may be created for an unknown preference, got %d creates", spy.createCall)
	}
}

func TestLaneFillCreateRollsBackWhenPreferenceFails(t *testing.T) {
	spy := newLaneSpy()
	spy.failSet = true
	s := &Server{autonomousMgr: spy}
	rec := postLanePreference(s, "/api/autonomous/prds", `{"spec":"x","project_dir":"/srv/x","lane_preference":"gpu"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a failed preference set on create must be 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(spy.deleted) != 1 || spy.deleted[0] != "new1" {
		t.Fatalf("the new Automaton must be rolled back, deleted=%v", spy.deleted)
	}
}

// laneFillSurface returns the autonomous.lane_fill block GET /api/config serves.
func laneFillSurface(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	var block map[string]json.RawMessage
	raw, ok := getAutonomousConfigMap(t, s)["lane_fill"]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks lane_fill")
	}
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("decode lane_fill: %v", err)
	}
	return block
}

func TestLaneFillConfigSurfaceShowsEffectiveDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	block := laneFillSurface(t, s)
	want := map[string]string{
		"mode":                "shadow",
		"gpu_lanes":           "2",
		"claude_lanes":        "3",
		"idle_alert_minutes":  "30",
		"decompose_ahead_min": "2",
		"poll_seconds":        "60",
		"gpu_backend":         "pi-prompt",
		"claude_backend":      "claude-code",
	}
	for field, value := range want {
		if got := strings.Trim(string(block[field]), `"`); got != value {
			t.Errorf("lane_fill.%s = %q, want %q", field, got, value)
		}
	}
}

func TestLaneFillConfigValidationTable(t *testing.T) {
	rows := []struct {
		field   string
		value   any
		invalid bool
	}{
		{"mode", "on", false},
		{"mode", "shadow", false},
		{"mode", "off", false},
		{"mode", "", false},
		{"mode", "maybe", true},
		{"mode", 3, true},
		{"gpu_lanes", 3, false},
		{"gpu_lanes", 0, false},
		{"gpu_lanes", -1, true},
		{"gpu_lanes", 1.5, true},
		{"gpu_lanes", "2", true},
		{"claude_lanes", 0, false},
		{"claude_lanes", -2, true},
		{"idle_alert_minutes", 0, false},
		{"idle_alert_minutes", -5, true},
		{"decompose_ahead_min", 4, false},
		{"decompose_ahead_min", -1, true},
		{"poll_seconds", 10, false},
		{"poll_seconds", -1, true},
		{"gpu_backend", "opencode", false},
		{"gpu_backend", 7, true},
		{"gpu_model", "qwen3.8:27b", false},
		{"gpu_model", true, true},
		{"claude_backend", "claude-code", false},
		{"claude_backend", []int{1}, true},
	}
	for _, row := range rows {
		key := "autonomous.lane_fill." + row.field
		t.Run(row.field+"/rest_config", func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			before := string(laneFillSurface(t, s)[row.field])
			b, _ := json.Marshal(map[string]any{key: row.value})
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
			if row.invalid {
				if rr.Code != http.StatusBadRequest {
					t.Fatalf("%v: want 400, got %d: %s", row.value, rr.Code, rr.Body.String())
				}
				if got := string(laneFillSurface(t, s)[row.field]); got != before {
					t.Fatalf("a rejected PUT changed %s: %s -> %s", row.field, before, got)
				}
				return
			}
			if rr.Code != http.StatusOK {
				t.Fatalf("%v: want 200, got %d: %s", row.value, rr.Code, rr.Body.String())
			}
		})
		t.Run(row.field+"/rest_autonomous_config", func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			b, _ := json.Marshal(map[string]any{"lane_fill": map[string]any{row.field: row.value}})
			rr := httptest.NewRecorder()
			s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(string(b))))
			if row.invalid && rr.Code != http.StatusBadRequest {
				t.Fatalf("%v: want 400, got %d: %s", row.value, rr.Code, rr.Body.String())
			}
			if !row.invalid && rr.Code != http.StatusOK {
				t.Fatalf("%v: want 200, got %d: %s", row.value, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestLaneFillConfigRejectsWholePUT(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{
		"autonomous.lane_fill.gpu_lanes":          4,
		"autonomous.lane_fill.idle_alert_minutes": -1,
	})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("one bad lane fill key must reject the whole PUT, got %d", rr.Code)
	}
	if got := string(laneFillSurface(t, s)["gpu_lanes"]); got != "2" {
		t.Fatalf("gpu_lanes changed by a rejected PUT: %s", got)
	}
	if mgr.Config().LaneFill.GPULanes != 2 {
		t.Fatalf("live manager gpu_lanes changed by a rejected PUT: %d", mgr.Config().LaneFill.GPULanes)
	}
}

func TestLaneFillConfigLiveApply(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.lane_fill.mode":               "on",
		"autonomous.lane_fill.gpu_lanes":          4,
		"autonomous.lane_fill.idle_alert_minutes": 45,
		"autonomous.lane_fill.poll_seconds":       15,
		"autonomous.lane_fill.gpu_backend":        "opencode",
		"autonomous.lane_fill.gpu_model":          "qwen3.8:27b",
	})
	block := laneFillSurface(t, s)
	if got := strings.Trim(string(block["mode"]), `"`); got != "on" {
		t.Fatalf("GET after PUT: mode = %q, want on", got)
	}
	if got := string(block["gpu_lanes"]); got != "4" {
		t.Fatalf("GET after PUT: gpu_lanes = %q, want 4", got)
	}
	live := mgr.Config().LaneFill
	if live.Mode != "on" || live.GPULanes != 4 || live.IdleAlertMinutes != 45 || live.PollSeconds != 15 ||
		live.GPUBackend != "opencode" || live.GPUModel != "qwen3.8:27b" {
		t.Fatalf("live manager did not receive the change: %+v", live)
	}
}

func TestLaneFillConfigLiveApplyAutonomousConfig(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config",
		map[string]any{"lane_fill": map[string]any{"claude_lanes": 1, "claude_backend": "claude-code"}})
	if live := mgr.Config().LaneFill; live.ClaudeLanes != 1 {
		t.Fatalf("live manager ClaudeLanes = %d, want 1", live.ClaudeLanes)
	}
}
