package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// supervisorWatchSpy is the double for the supervisor watch read route: a
// fixed read view and a count of read calls so the tests can prove a
// refused request never reaches the manager.
type supervisorWatchSpy struct {
	fakeOrchAutonomous
	view  autonomous.SupervisorWatchStatus
	calls int
}

func (s *supervisorWatchSpy) SupervisorWatchStatus() autonomous.SupervisorWatchStatus {
	s.calls++
	return s.view
}

func supervisorWatchGET(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSupervisorWatch(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/supervisor-watch", nil))
	return rec
}

func supervisorWatchBaseTime() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }

// supervisorWatchFixtureView is the read view the route serves: two open
// findings (one shadow-marked alert-due, one with a raised alert) and a
// comparison view carrying one last comparison with a one-sided
// difference.
func supervisorWatchFixtureView() autonomous.SupervisorWatchStatus {
	base := supervisorWatchBaseTime()
	alertedAt := base.Add(40 * time.Minute)
	return autonomous.SupervisorWatchStatus{
		Mode:       autonomous.SupervisorWatchModeShadow,
		LastPollAt: base.Add(-time.Minute),
		Findings: []*autonomous.SupervisorWatchFinding{
			{
				Kind:        autonomous.SupervisorWatchTaskFailed,
				Subject:     "1001",
				Text:        "Automaton 1001 task failed: its last task ended non-zero",
				FirstSeenAt: base,
				LastSeenAt:  base.Add(time.Minute),
				WouldAlert:  true,
			},
			{
				Kind:        autonomous.SupervisorWatchCIRed,
				Subject:     "42",
				Text:        "CI run 42 is red on main",
				FirstSeenAt: base.Add(-50 * time.Minute),
				LastSeenAt:  base.Add(2 * time.Minute),
				AlertedAt:   &alertedAt,
			},
		},
		Comparison: autonomous.SupervisorWatchComparisonView{
			Last: &autonomous.SupervisorWatchComparison{
				At:          base.Add(-time.Minute),
				WatchKinds:  []string{"task_failed/1001"},
				ScriptKinds: []string{},
				OnlyWatch:   []string{"task_failed/1001"},
				OnlyScript:  []string{},
				Agreed:      false,
			},
			AgreedDays: 2,
			OnlyWatch:  []string{"task_failed/1001"},
			OnlyScript: []string{},
		},
	}
}

func supervisorWatchFixtureServer() (*Server, *supervisorWatchSpy) {
	spy := &supervisorWatchSpy{view: supervisorWatchFixtureView()}
	return &Server{autonomousMgr: spy}, spy
}

// TestSupervisorWatchRESTEmptyStore — an empty store reads as an empty
// findings list and an empty comparison view, never a 404 and never
// nulls where the shape promises a list.
func TestSupervisorWatchRESTEmptyStore(t *testing.T) {
	spy := &supervisorWatchSpy{view: autonomous.SupervisorWatchStatus{
		Mode:       autonomous.SupervisorWatchModeShadow,
		Findings:   []*autonomous.SupervisorWatchFinding{},
		Comparison: autonomous.SupervisorWatchComparisonView{OnlyWatch: []string{}, OnlyScript: []string{}},
	}}
	s := &Server{autonomousMgr: spy}
	rec := supervisorWatchGET(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"findings":[]`) {
		t.Errorf("body lacks the empty findings list: %s", body)
	}
	if !strings.Contains(body, `"last":null`) {
		t.Errorf("body lacks the null last comparison: %s", body)
	}
	if !strings.Contains(body, `"agreed_days":0`) {
		t.Errorf("body lacks agreed_days 0: %s", body)
	}
	if spy.calls != 1 {
		t.Errorf("manager read calls = %d, want exactly one", spy.calls)
	}
}

// TestSupervisorWatchRESTPopulated — the stored findings come back with
// their identity, the operator line, the seen instants and the alert
// marks intact, and the comparison view carries the last comparison.
func TestSupervisorWatchRESTPopulated(t *testing.T) {
	s, _ := supervisorWatchFixtureServer()
	rec := supervisorWatchGET(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Mode       string `json:"mode"`
		LastPollAt string `json:"last_poll_at"`
		Findings   []struct {
			Kind       string  `json:"kind"`
			Subject    string  `json:"subject"`
			Text       string  `json:"text"`
			FirstSeen  string  `json:"first_seen_at"`
			LastSeen   string  `json:"last_seen_at"`
			WouldAlert bool    `json:"would_alert"`
			AlertedAt  *string `json:"alerted_at"`
		} `json:"findings"`
		Comparison struct {
			Last *struct {
				Agreed bool `json:"agreed"`
			} `json:"last"`
			AgreedDays int      `json:"agreed_days"`
			OnlyWatch  []string `json:"only_watch"`
			OnlyScript []string `json:"only_script"`
		} `json:"comparison"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}

	if body.LastPollAt == "" {
		t.Errorf("last_poll_at is empty; want the coordinator's last poll instant")
	}
	if len(body.Findings) != 2 {
		t.Fatalf("findings = %d, want both stored findings", len(body.Findings))
	}
	failed, red := body.Findings[0], body.Findings[1]
	if failed.Kind != "task_failed" || failed.Subject != "1001" {
		t.Errorf("finding 0 = %q/%q, want task_failed/1001", failed.Kind, failed.Subject)
	}
	if failed.Text == "" {
		t.Errorf("finding 0 lost its operator line")
	}
	if failed.FirstSeen == "" || failed.LastSeen == "" {
		t.Errorf("finding 0 lost its seen instants")
	}
	if !failed.WouldAlert {
		t.Errorf("finding 0 lost its shadow would-alert mark")
	}
	if red.Kind != "ci_red" || red.Subject != "42" {
		t.Errorf("finding 1 = %q/%q, want ci_red/42", red.Kind, red.Subject)
	}
	if red.AlertedAt == nil {
		t.Errorf("finding 1 lost its alerted_at")
	}

	if body.Comparison.Last == nil {
		t.Errorf("comparison.last is null; want the stored last comparison")
	}
	if body.Comparison.Last != nil && body.Comparison.Last.Agreed {
		t.Errorf("comparison.last.agreed = true, want false")
	}
	if body.Comparison.AgreedDays != 2 {
		t.Errorf("comparison.agreed_days = %d, want 2", body.Comparison.AgreedDays)
	}
	if strings.Join(body.Comparison.OnlyWatch, ",") != "task_failed/1001" {
		t.Errorf("comparison.only_watch = %v, want [task_failed/1001]", body.Comparison.OnlyWatch)
	}
	if body.Comparison.OnlyScript == nil || len(body.Comparison.OnlyScript) != 0 {
		t.Errorf("comparison.only_script = %v, want an empty list", body.Comparison.OnlyScript)
	}
}

// TestSupervisorWatchRESTPriorityBands — the per-band wait stats ride in
// the status payload: the rows come back in high, medium, low order with
// their band, range, waiting count, oldest wait and stalled share intact,
// and a view where no band waits anything reads an empty list (never
// null) so a caller always gets a list.
func TestSupervisorWatchRESTPriorityBands(t *testing.T) {
	spy := &supervisorWatchSpy{view: autonomous.SupervisorWatchStatus{
		Mode:     autonomous.SupervisorWatchModeShadow,
		Findings: []*autonomous.SupervisorWatchFinding{},
		Comparison: autonomous.SupervisorWatchComparisonView{
			OnlyWatch:  []string{},
			OnlyScript: []string{},
		},
		PriorityBands: []autonomous.SupervisorWatchPriorityBand{
			{Band: "high", PriorityFrom: 80, PriorityTo: 100, Waiting: 2, OldestWaitingMinutes: 41, Stalled: 1},
			{Band: "medium", PriorityFrom: 50, PriorityTo: 79, Waiting: 1, OldestWaitingMinutes: 22, Stalled: 0},
			{Band: "low", PriorityFrom: 0, PriorityTo: 49, Waiting: 3, OldestWaitingMinutes: 5, Stalled: 0},
		},
	}}
	s := &Server{autonomousMgr: spy}
	rec := supervisorWatchGET(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		PriorityBands []struct {
			Band                 string `json:"band"`
			PriorityFrom         int    `json:"priority_from"`
			PriorityTo           int    `json:"priority_to"`
			Waiting              int    `json:"waiting"`
			OldestWaitingMinutes int    `json:"oldest_waiting_minutes"`
			Stalled              int    `json:"stalled"`
		} `json:"priority_bands"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if len(body.PriorityBands) != 3 {
		t.Fatalf("priority_bands = %+v, want all three bands", body.PriorityBands)
	}
	high, medium, low := body.PriorityBands[0], body.PriorityBands[1], body.PriorityBands[2]
	if high.Band != "high" || medium.Band != "medium" || low.Band != "low" {
		t.Errorf("band order = %q, %q, %q, want high, medium, low", high.Band, medium.Band, low.Band)
	}
	if high.PriorityFrom != 80 || high.PriorityTo != 100 || high.Waiting != 2 || high.OldestWaitingMinutes != 41 || high.Stalled != 1 {
		t.Errorf("high row = %+v, want 80..100 / 2 waiting / 41 min / 1 stalled", high)
	}
	if medium.PriorityFrom != 50 || medium.PriorityTo != 79 || medium.Waiting != 1 || medium.OldestWaitingMinutes != 22 {
		t.Errorf("medium row = %+v, want 50..79 / 1 waiting / 22 min", medium)
	}
	if low.PriorityFrom != 0 || low.PriorityTo != 49 || low.Waiting != 3 || low.OldestWaitingMinutes != 5 || low.Stalled != 0 {
		t.Errorf("low row = %+v, want 0..49 / 3 waiting / 5 min / 0 stalled", low)
	}

	// a view with no band waiting anything reads an empty list, never
	// null — the read surface always gets a list
	emptySpy := &supervisorWatchSpy{view: autonomous.SupervisorWatchStatus{
		Mode:          autonomous.SupervisorWatchModeShadow,
		Findings:      []*autonomous.SupervisorWatchFinding{},
		PriorityBands: []autonomous.SupervisorWatchPriorityBand{},
		Comparison:    autonomous.SupervisorWatchComparisonView{OnlyWatch: []string{}, OnlyScript: []string{}},
	}}
	rec = supervisorWatchGET(t, &Server{autonomousMgr: emptySpy})
	if rec.Code != http.StatusOK {
		t.Fatalf("empty GET: want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"priority_bands":[]`) {
		t.Errorf("empty view must carry an empty band list, not null: %s", rec.Body)
	}

	// and the stats GET carries the same rows — both read endpoints
	// serve the band stats, not only one of them
	statsSpy := &supervisorWatchStatsSpy{stats: autonomous.SupervisorWatchStats{
		Mode: "shadow",
		PriorityBands: []autonomous.SupervisorWatchPriorityBand{
			{Band: "high", PriorityFrom: 80, PriorityTo: 100, Waiting: 2, OldestWaitingMinutes: 41, Stalled: 1},
			{Band: "low", PriorityFrom: 0, PriorityTo: 49, Waiting: 1, OldestWaitingMinutes: 5, Stalled: 0},
		},
	}}
	rec = supervisorWatchStatsGET(&Server{autonomousMgr: statsSpy}, context.Background())
	if rec.Code != http.StatusOK {
		t.Fatalf("stats GET: want 200, got %d", rec.Code)
	}
	for _, want := range []string{
		`"priority_bands":[`,
		`{"band":"high","priority_from":80,"priority_to":100,"waiting":2,"oldest_waiting_minutes":41,"stalled":1}`,
		`{"band":"low","priority_from":0,"priority_to":49,"waiting":1,"oldest_waiting_minutes":5,"stalled":0}`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("stats body lost %s: %s", want, rec.Body)
		}
	}
}

// TestSupervisorWatchRESTModeInPayload — the mode in effect rides in the
// payload, shadow and on both, and is what the payload says.
func TestSupervisorWatchRESTModeInPayload(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{autonomous.SupervisorWatchModeShadow, "shadow"},
		{autonomous.SupervisorWatchModeOn, "on"},
		{autonomous.SupervisorWatchModeOff, "off"},
	}
	for _, c := range cases {
		spy := &supervisorWatchSpy{view: autonomous.SupervisorWatchStatus{Mode: c.mode}}
		s := &Server{autonomousMgr: spy}
		rec := supervisorWatchGET(t, s)
		if rec.Code != http.StatusOK {
			t.Fatalf("mode %q: want 200, got %d", c.mode, rec.Code)
		}
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("mode %q: decode: %v", c.mode, err)
		}
		if body.Mode != c.want {
			t.Errorf("mode = %q, want %q", body.Mode, c.want)
		}
	}
}

// TestSupervisorWatchRESTMethodNotAllowed — the route is read-only:
// POST (and the other write methods) is refused with a 405 before the
// manager is read.
func TestSupervisorWatchRESTMethodNotAllowed(t *testing.T) {
	s, spy := supervisorWatchFixtureServer()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleSupervisorWatch(rec, httptest.NewRequest(method, "/api/autonomous/supervisor-watch", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	if spy.calls != 0 {
		t.Errorf("refused methods must not reach the manager, got %d calls", spy.calls)
	}
}

// TestSupervisorWatchRESTAuthentication — the capability gate behaves
// like the sibling read routes: without the autonomous read capability
// the request is refused with a 403 naming it, with it the same request
// is served.
func TestSupervisorWatchRESTAuthentication(t *testing.T) {
	s, _ := supervisorWatchFixtureServer()

	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/supervisor-watch", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleSupervisorWatch(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/supervisor-watch", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleSupervisorWatch(rec, allow)
	if rec.Code != http.StatusOK {
		t.Errorf("with the read capability: want 200, got %d", rec.Code)
	}

	// the manager is still down when autonomous is disabled, like the
	// sibling routes
	off := &Server{}
	rec = supervisorWatchGET(t, off)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("autonomous disabled: want 503, got %d", rec.Code)
	}
}

// TestSupervisorWatchPriorityStallConfigRoundTrip — the two new supervisor
// watch keys as the REST config surface: an unset key reads its one default
// on GET /api/config (30 for stall_threshold_minutes, 80 for
// priority_floor, via the Effective* accessors — never a zero that reads a
// different default); a value written through PUT /api/config sticks in the
// loaded config, persists to the YAML, reads back effective on GET, and
// reaches the live autonomous manager inside the supervisor_watch block the
// push path carries explicitly; an explicit 0 round-trips as 0 (kept, not
// defaulted over); a negative value is refused with a 400 naming the full
// key and the previous value is kept.
func TestSupervisorWatchPriorityStallConfigRoundTrip(t *testing.T) {
	cfg := &config.Config{}
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml"), autonomousMgr: spy}

	put := func(t *testing.T, key string, v interface{}) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]interface{}{
			"autonomous.automation.supervisor_watch." + key: v,
		})
		if err != nil {
			t.Fatalf("marshal patch: %v", err)
		}
		req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handlePutConfig(rec, req)
		return rec
	}

	getBlock := func(t *testing.T) map[string]interface{} {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleGetConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/config: want 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var got struct {
			Autonomous struct {
				Automation struct {
					Watch map[string]interface{} `json:"supervisor_watch"`
				} `json:"automation"`
			} `json:"autonomous"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		return got.Autonomous.Automation.Watch
	}

	watchVal := func(t *testing.T, block map[string]interface{}, key string) int {
		t.Helper()
		f, ok := block[key].(float64)
		if !ok {
			t.Fatalf("GET %s is %v (%T), want a JSON number", key, block[key], block[key])
		}
		return int(f)
	}

	// Before any write: both keys are unset and read their one defaults.
	block := getBlock(t)
	if got := watchVal(t, block, "stall_threshold_minutes"); got != 30 {
		t.Errorf("unset GET stall_threshold_minutes = %d, want the 30 default", got)
	}
	if got := watchVal(t, block, "priority_floor"); got != 80 {
		t.Errorf("unset GET priority_floor = %d, want the 80 default", got)
	}

	// Write 45 / 80: they stick in the loaded config (as pointers, so a
	// later 0 can distinguish itself), persist to the YAML, read back
	// effective on GET, and reach the live manager.
	for key, want := range map[string]int{"stall_threshold_minutes": 45, "priority_floor": 80} {
		rec := put(t, key, want)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s=%d: want 200, got %d: %s", key, want, rec.Code, rec.Body.String())
		}
	}
	w := cfg.Autonomous.Automation.SupervisorWatch
	if w.StallThresholdMinutes == nil || *w.StallThresholdMinutes != 45 {
		t.Errorf("stored StallThresholdMinutes = %v, want 45", w.StallThresholdMinutes)
	}
	if w.PriorityFloor == nil || *w.PriorityFloor != 80 {
		t.Errorf("stored PriorityFloor = %v, want 80", w.PriorityFloor)
	}
	disk, err := os.ReadFile(s.cfgPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	for _, want := range []string{"stall_threshold_minutes: 45", "priority_floor: 80"} {
		if !strings.Contains(string(disk), want) {
			t.Errorf("saved config.yaml lacks %q:\n%s", want, disk)
		}
	}
	block = getBlock(t)
	if got := watchVal(t, block, "stall_threshold_minutes"); got != 45 {
		t.Errorf("GET stall_threshold_minutes after PUT = %d, want 45", got)
	}
	if got := watchVal(t, block, "priority_floor"); got != 80 {
		t.Errorf("GET priority_floor after PUT = %d, want 80", got)
	}

	// The live manager's supervisor_watch block carries both keys
	// explicitly (the push path writes them so a clear-to-0 is visible to
	// the live merge) at the written values.
	if spy.calls == 0 {
		t.Fatalf("the live autonomous manager was never configured; want the PUT to forward the change")
	}
	var forwarded struct {
		Automation struct {
			Watch map[string]json.RawMessage `json:"supervisor_watch"`
		} `json:"automation"`
	}
	if err := json.Unmarshal(spy.lastSetConfig, &forwarded); err != nil {
		t.Fatalf("decode forwarded config: %v (%s)", err, spy.lastSetConfig)
	}
	for key, want := range map[string]int{"stall_threshold_minutes": 45, "priority_floor": 80} {
		raw, ok := forwarded.Automation.Watch[key]
		if !ok {
			t.Errorf("forwarded supervisor_watch block lacks %q: %s", key, spy.lastSetConfig)
			continue
		}
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Errorf("forwarded %s = %s, want a whole number", key, raw)
			continue
		}
		if n != want {
			t.Errorf("forwarded %s = %d, want %d", key, n, want)
		}
	}

	// A clear-to-0 round-trips as 0: the explicit zero is kept, not
	// defaulted over to 30 / 80.
	for _, key := range []string{"stall_threshold_minutes", "priority_floor"} {
		rec := put(t, key, 0)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s=0: want 200, got %d: %s", key, rec.Code, rec.Body.String())
		}
	}
	w = cfg.Autonomous.Automation.SupervisorWatch
	if w.StallThresholdMinutes == nil || *w.StallThresholdMinutes != 0 {
		t.Errorf("cleared StallThresholdMinutes = %v, want a kept explicit 0", w.StallThresholdMinutes)
	}
	if w.PriorityFloor == nil || *w.PriorityFloor != 0 {
		t.Errorf("cleared PriorityFloor = %v, want a kept explicit 0", w.PriorityFloor)
	}
	if got := w.EffectiveStallThresholdMinutes(); got != 0 {
		t.Errorf("cleared stall: effective = %d, want 0 (not the default)", got)
	}
	if got := w.EffectivePriorityFloor(); got != 0 {
		t.Errorf("cleared floor: effective = %d, want 0 (not the default)", got)
	}
	block = getBlock(t)
	if got := watchVal(t, block, "stall_threshold_minutes"); got != 0 {
		t.Errorf("GET stall_threshold_minutes after clear = %d, want 0", got)
	}
	if got := watchVal(t, block, "priority_floor"); got != 0 {
		t.Errorf("GET priority_floor after clear = %d, want 0", got)
	}

	// A negative value is refused with a 400 naming the full
	// autonomous.automation.supervisor_watch.<key> path, and the previous
	// (0) value is kept — the refusal is not a silent clear either.
	for _, key := range []string{"stall_threshold_minutes", "priority_floor"} {
		rec := put(t, key, -1)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s=-1: want 400, got %d: %s", key, rec.Code, rec.Body.String())
		}
		full := "autonomous.automation.supervisor_watch." + key
		if !strings.Contains(rec.Body.String(), full) {
			t.Errorf("%s=-1 refusal must name %s, got %q", key, full, rec.Body.String())
		}
	}
	w = cfg.Autonomous.Automation.SupervisorWatch
	if w.StallThresholdMinutes == nil || *w.StallThresholdMinutes != 0 {
		t.Errorf("after refused negatives, StallThresholdMinutes = %v, want the previous 0 kept", w.StallThresholdMinutes)
	}
	if w.PriorityFloor == nil || *w.PriorityFloor != 0 {
		t.Errorf("after refused negatives, PriorityFloor = %v, want the previous 0 kept", w.PriorityFloor)
	}
}
