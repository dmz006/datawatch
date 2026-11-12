// Tests for the operator console one-glance summary (api_dashboard_console.go).

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// consoleQuestionSpy fakes the operator question channel (QuestionsAPI).
type consoleQuestionSpy struct {
	qs   []autonomous.Question
	list autonomous.QuestionFilter
}

func (q *consoleQuestionSpy) Raise(autonomous.Question) (autonomous.Question, error) {
	return autonomous.Question{}, nil
}
func (q *consoleQuestionSpy) Get(string) (autonomous.Question, bool) {
	return autonomous.Question{}, false
}
func (q *consoleQuestionSpy) List(f autonomous.QuestionFilter) []autonomous.Question {
	q.list = f
	return q.qs
}
func (q *consoleQuestionSpy) Answer(string, autonomous.Answer) (autonomous.Answer, error) {
	return autonomous.Answer{}, nil
}
func (q *consoleQuestionSpy) Withdraw(string, string, string) (autonomous.Question, error) {
	return autonomous.Question{}, nil
}
func (q *consoleQuestionSpy) Answers(string) ([]autonomous.AnswerView, bool) {
	return nil, false
}
func (q *consoleQuestionSpy) CurrentAnswer(string) (autonomous.Answer, bool) {
	return autonomous.Answer{}, false
}
func (q *consoleQuestionSpy) AnswerCount(string) int { return 0 }

// consoleSpy fakes the autonomous manager surface the console reads:
// triage records, uncommitted-work holds, lane fill and the repo CI watch.
// The AutonomousAPI shape comes from the existing fakeOrchAutonomous; the
// console-specific reader interfaces are overridden here.
type consoleSpy struct {
	fakeOrchAutonomous
	triage []*autonomous.TriageRecord
	held   []autonomous.UncommittedWorkEntry
	lanes  []autonomous.LaneFillLaneStatus
	mode   string
	ci     autonomous.RepoCIWatchStatus
}

func (c *consoleSpy) ListTriageRecords() []*autonomous.TriageRecord { return c.triage }
func (c *consoleSpy) TriageAlertMinutes() int                       { return 0 }
func (c *consoleSpy) UncommittedWorkStatus(string) autonomous.UncommittedWorkStatus {
	return autonomous.UncommittedWorkStatus{Held: c.held}
}
func (c *consoleSpy) LaneFillStatus(int) autonomous.LaneFillStatus {
	return autonomous.LaneFillStatus{Mode: c.mode, Lanes: c.lanes}
}
func (c *consoleSpy) RepoCIWatchStatus() autonomous.RepoCIWatchStatus { return c.ci }

func consoleFixtureServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Autonomous.Questions.Enabled = true // the zero config leaves the channel off
	qs := []autonomous.Question{
		{ID: "q1", Text: "what to do", Blocking: true, Source: autonomous.QuestionSourceHeld},
		{ID: "q2", Text: "which option", Blocking: true, Source: autonomous.QuestionSourceRest},
		{ID: "q3", Text: "informational", Blocking: false, Source: autonomous.QuestionSourceRest},
	}
	spy := &consoleSpy{
		triage: []*autonomous.TriageRecord{
			{PRDID: "p1", TaskTitle: "a task", Status: "failed"},
			{PRDID: "p2", TaskTitle: "another task", Status: "blocked"},
		},
		held: []autonomous.UncommittedWorkEntry{{AutomatonID: "a1"}},
		lanes: []autonomous.LaneFillLaneStatus{
			{ID: "lane-a", State: "occupied"},
			{ID: "lane-b", State: "idle"},
		},
		mode: "shadow",
		ci: autonomous.RepoCIWatchStatus{
			Mode:  "off",
			Repos: []string{"repo-a", "repo-b"},
			Records: []*autonomous.RepoCIWatchRecord{
				{Status: "green"},
				{Status: "failing"},
				{Status: "green"},
			},
		},
	}
	s := &Server{cfg: cfg, autonomousMgr: spy, questionStore: &consoleQuestionSpy{qs: qs}}
	g, err := NewBurnGuard(BurnGuardInputs{
		Config: func() config.BurnGuardConfig { return config.BurnGuardConfig{} },
		Readings: func(context.Context) ([]usagecalc.Reading, float64, usagecalc.ResetSpec, error) {
			return nil, 0, usagecalc.ResetSpec{}, nil
		},
	}, burnGuardAdmNull{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewBurnGuard: %v", err)
	}
	g.mu.Lock()
	g.status = BurnGuardStatus{
		Enabled: true,
		State:   "guarding",
		LastProjection: &burnguard.Projection{
			Target:    85,
			Projected: 90,
			Over:      true,
			OverBy:    5,
		},
	}
	g.mu.Unlock()
	s.burnGuard = g
	s.usageCalc = consoleUsageService(t)
	return s
}

// consoleUsageService builds a usage calculator over one seeded reading (40%),
// the same recipe usagecalc's own service tests use.
func consoleUsageService(t *testing.T) *usagecalc.Service {
	t.Helper()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	fixed := func() time.Time { return now }
	store, err := usagecalc.OpenStore(t.TempDir(), fixed)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	pct, ln := 40.0, 2
	if _, err := store.Append(usagecalc.Reading{
		At:          time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC),
		Percent:     &pct,
		Lanes:       &ln,
		LanesSource: usagecalc.LanesLive,
		Source:      usagecalc.SourceTyped,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	series := usagecalc.NewSharedSeries(store, "", nil, usagecalc.WithSharedClock(fixed))
	return usagecalc.NewService(series, store,
		func() usagecalc.Settings {
			return usagecalc.Settings{Reset: usagecalc.ResetSpec{Weekday: time.Sunday, Clock: "08:00", Zone: "UTC"}}
		},
		nil, usagecalc.WithServiceNow(fixed))
}

// burnGuardAdmNull is the admission seam NewBurnGuard requires; the console
// never acts through it.
type burnGuardAdmNull struct{}

func (burnGuardAdmNull) InflightCap(string) int                  { return 0 }
func (burnGuardAdmNull) SetInflight(string, int) error           { return nil }
func (burnGuardAdmNull) RouteBackend(string, string, bool) error { return nil }
func (burnGuardAdmNull) HoldNewWork(string, bool) error          { return nil }

func getConsole(t *testing.T, s *Server) (int, dashboardConsoleResponse) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/console", nil)
	s.handleDashboardConsole(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/dashboard/console: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp dashboardConsoleResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return rr.Code, resp
}

func TestDashboardConsoleHappyPathAllSectionsAvailable(t *testing.T) {
	s := consoleFixtureServer(t)
	_, resp := getConsole(t, s)

	want := map[string]consoleSection{
		"questions_waiting": resp.QuestionsWaiting,
		"held_blocked":      resp.HeldBlocked,
		"lanes":             resp.Lanes,
		"usage":             resp.Usage,
		"ci_release":        resp.CIRelase,
	}
	for name, sec := range want {
		if !sec.Available {
			t.Errorf("%s: want available, got available=false reason=%q", name, sec.Reason)
			continue
		}
		if sec.Reason != "" {
			t.Errorf("%s: available section with reason %q", name, sec.Reason)
		}
		if sec.Link == "" {
			t.Errorf("%s: available section without a link", name)
		}
	}

	if resp.QuestionsWaiting.Count != 3 {
		t.Errorf("questions_waiting count: got %d, want 3 (the open questions)", resp.QuestionsWaiting.Count)
	}
	if !strings.Contains(resp.QuestionsWaiting.Summary, "2 blocking") {
		t.Errorf("questions_waiting summary: %q (want it to name 2 blocking)", resp.QuestionsWaiting.Summary)
	}
	if !strings.Contains(resp.QuestionsWaiting.Summary, "1 held") {
		t.Errorf("questions_waiting summary: %q (want it to name 1 held)", resp.QuestionsWaiting.Summary)
	}
	if resp.QuestionsWaiting.Link != consoleAnchorQuestions {
		t.Errorf("questions_waiting link: got %q, want %q", resp.QuestionsWaiting.Link, consoleAnchorQuestions)
	}

	if resp.HeldBlocked.Count != 2 {
		t.Errorf("held_blocked count: got %d, want 2 (the failed/blocked automata)", resp.HeldBlocked.Count)
	}
	if !strings.Contains(resp.HeldBlocked.Summary, "1 failed") || !strings.Contains(resp.HeldBlocked.Summary, "1 blocked") {
		t.Errorf("held_blocked summary: %q (want the failed/blocked split)", resp.HeldBlocked.Summary)
	}
	if !strings.Contains(resp.HeldBlocked.Summary, "1 uncommitted-work hold") {
		t.Errorf("held_blocked summary: %q (want the uncommitted-work hold)", resp.HeldBlocked.Summary)
	}

	if resp.Lanes.Count != 2 {
		t.Errorf("lanes count: got %d, want 2 (the lanes)", resp.Lanes.Count)
	}
	if !strings.Contains(resp.Lanes.Summary, "1 of 2 lane(s) in use") {
		t.Errorf("lanes summary: %q (want the lane split)", resp.Lanes.Summary)
	}

	if resp.Usage.Count != 40 {
		t.Errorf("usage count: got %d, want 40 (the weekly percent at the reading)", resp.Usage.Count)
	}
	if !strings.Contains(resp.Usage.Summary, "burn guard guarding (level 0)") {
		t.Errorf("usage summary: %q (want the burn guard state)", resp.Usage.Summary)
	}
	if !strings.Contains(resp.Usage.Summary, "40% of the weekly window used") {
		t.Errorf("usage summary: %q (want the usage percent)", resp.Usage.Summary)
	}

	if resp.CIRelase.Count != 3 {
		t.Errorf("ci_release count: got %d, want 3 (the CI runs under watch)", resp.CIRelase.Count)
	}
	if !strings.Contains(resp.CIRelase.Summary, "1 failing or hung") {
		t.Errorf("ci_release summary: %q (want the failing/hung split)", resp.CIRelase.Summary)
	}
	if !strings.Contains(resp.CIRelase.Summary, "across 2 repo(s)") {
		t.Errorf("ci_release summary: %q (want the repo split)", resp.CIRelase.Summary)
	}

	if resp.GeneratedAt.IsZero() {
		t.Errorf("generated_at: zero value, want the response time")
	}
}

func TestDashboardConsoleMethodNotAllowed(t *testing.T) {
	s := consoleFixtureServer(t)
	rr := httptest.NewRecorder()
	s.handleDashboardConsole(rr, httptest.NewRequest(http.MethodPost, "/api/dashboard/console", nil))
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("POST: want 501, got %d", rr.Code)
	}
}

func TestDashboardConsoleDisabledHidesEverySection(t *testing.T) {
	s := consoleFixtureServer(t)
	off := false
	s.cfg.Dashboard.Console.Enabled = &off
	_, resp := getConsole(t, s)

	for name, sec := range map[string]consoleSection{
		"questions_waiting": resp.QuestionsWaiting,
		"held_blocked":      resp.HeldBlocked,
		"lanes":             resp.Lanes,
		"usage":             resp.Usage,
		"ci_release":        resp.CIRelase,
	} {
		if sec.Available {
			t.Errorf("%s: with the console disabled, want available=false", name)
		}
		if !strings.Contains(sec.Reason, "dashboard.console.enabled") {
			t.Errorf("%s: reason %q (want it to name dashboard.console.enabled)", name, sec.Reason)
		}
	}
}

func TestDashboardConsoleShowSwitchHidesOneSection(t *testing.T) {
	s := consoleFixtureServer(t)
	off := false
	s.cfg.Dashboard.Console.ShowLanes = &off
	s.cfg.Dashboard.Console.ShowUsage = &off
	_, resp := getConsole(t, s)

	if resp.Lanes.Available {
		t.Errorf("lanes: with show_lanes=false want available=false, got %s", resp.Lanes.Summary)
	}
	if !strings.Contains(resp.Lanes.Reason, "dashboard.console.show_lanes") {
		t.Errorf("lanes reason: %q (want it to name dashboard.console.show_lanes)", resp.Lanes.Reason)
	}
	if resp.Usage.Available {
		t.Errorf("usage: with show_usage=false want available=false")
	}
	if !strings.Contains(resp.Usage.Reason, "dashboard.console.show_usage") {
		t.Errorf("usage reason: %q (want it to name dashboard.console.show_usage)", resp.Usage.Reason)
	}
	if !resp.QuestionsWaiting.Available || !resp.HeldBlocked.Available || !resp.CIRelase.Available {
		t.Errorf("the untouched sections must stay available: %+v", resp)
	}
}

func TestDashboardConsoleUnwiredSubsystemsNever5xx(t *testing.T) {
	// No managers, stores, guards or config at all: every section reports
	// itself unavailable, the endpoint still answers 200.
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleDashboardConsole(rr, httptest.NewRequest(http.MethodGet, "/api/dashboard/console", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 for an all-unwired daemon, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp dashboardConsoleResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for name, sec := range map[string]consoleSection{
		"questions_waiting": resp.QuestionsWaiting,
		"held_blocked":      resp.HeldBlocked,
		"lanes":             resp.Lanes,
		"usage":             resp.Usage,
		"ci_release":        resp.CIRelase,
	} {
		if sec.Available {
			t.Errorf("%s: with nothing wired, want available=false", name)
		}
		if sec.Reason == "" {
			t.Errorf("%s: unavailable without a reason the operator can act on", name)
		}
		if sec.Count != 0 {
			t.Errorf("%s: unavailable section with count %d", name, sec.Count)
		}
	}
}

func TestDashboardConsoleQuestionsDisabledReportsTheSwitch(t *testing.T) {
	s := consoleFixtureServer(t)
	s.cfg.Autonomous.Questions.Enabled = false
	_, resp := getConsole(t, s)
	if resp.QuestionsWaiting.Available {
		t.Fatalf("questions_waiting: with autonomous.questions.enabled=false want available=false")
	}
	if !strings.Contains(resp.QuestionsWaiting.Reason, "autonomous.questions.enabled") {
		tf := "questions_waiting reason: %q (want it to name autonomous.questions.enabled)"
		t.Errorf(tf, resp.QuestionsWaiting.Reason)
	}
	if !resp.HeldBlocked.Available {
		t.Error("held_blocked must stay available (different subsystem)")
	}
}

func TestDashboardConsoleUsageUnavailableWhenOnlyCalculatorIsBroken(t *testing.T) {
	s := consoleFixtureServer(t)
	// A calculator with no readings series: Advice refuses; with no burn
	// guard behind it the section reports unavailable instead of 5xx'ing.
	s.burnGuard = nil
	s.usageCalc = usagecalc.NewService(nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	s.handleDashboardConsole(rr, httptest.NewRequest(http.MethodGet, "/api/dashboard/console", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp dashboardConsoleResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Usage.Available {
		t.Fatalf("usage: a broken calculator want available=false, got %q", resp.Usage.Summary)
	}
	if !strings.Contains(resp.Usage.Reason, "advice") {
		t.Errorf("usage reason: %q (want it to name the unavailable advice)", resp.Usage.Reason)
	}
}

func TestDashboardConsoleConfigGetPUTRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}

	get := func(key string) (bool, bool) {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d", rr.Code)
		}
		var got struct {
			Dashboard struct {
				Console map[string]bool `json:"console"`
			} `json:"dashboard"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		v, ok := got.Dashboard.Console[key]
		return v, ok
	}

	put := func(key string, val bool) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"dashboard.console." + key: val})
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b)))
		s.handlePutConfig(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %s=%v: want 200, got %d body=%s", key, val, rr.Code, rr.Body.String())
		}
	}

	// Every key defaults to true on a fresh config.
	for _, key := range []string{"enabled", "show_questions", "show_held_blocked", "show_lanes", "show_usage", "show_ci_release"} {
		v, ok := get(key)
		if !ok {
			t.Errorf("GET dashboard.console.%s: key absent from the dashboard block", key)
			continue
		}
		if !v {
			t.Errorf("GET dashboard.console.%s before any PUT: want the default true", key)
		}
	}
	// An explicit false sticks, the others keep their defaults.
	put("show_usage", false)
	if v, _ := get("show_usage"); v {
		t.Errorf("after PUT show_usage=false: got true")
	}
	if v, _ := get("show_lanes"); !v {
		t.Errorf("show_lanes must keep its default true after an unrelated PUT")
	}
	put("show_usage", true)
	if v, _ := get("show_usage"); !v {
		t.Errorf("after PUT show_usage=true: got false")
	}
}

func TestPlatformContextConfigGetPUTRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}

	get := func() map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		pc, _ := got["platform_context"].(map[string]any)
		if pc == nil {
			t.Fatalf("GET: platform_context block absent")
		}
		if len(pc) != 2 {
			t.Errorf("platform_context must expose only enabled and note, got %v", pc)
		}
		return pc
	}
	put := func(patch map[string]any) int {
		t.Helper()
		b, _ := json.Marshal(patch)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		return rr.Code
	}

	pc := get()
	if pc["enabled"] != true || pc["note"] != "" {
		t.Errorf("defaults: %v", pc)
	}
	if code := put(map[string]any{"platform_context.enabled": false, "platform_context.note": "extra"}); code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d", code)
	}
	pc = get()
	if pc["enabled"] != false || pc["note"] != "extra" {
		t.Errorf("after PUT: %v", pc)
	}
	long := strings.Repeat("x", config.PlatformContextNoteMaxLen+1)
	if code := put(map[string]any{"platform_context.note": long}); code != http.StatusBadRequest {
		t.Errorf("over-long note: want 400, got %d", code)
	}
	if pc = get(); pc["note"] != "extra" {
		t.Errorf("rejected PUT changed the note: %v", pc["note"])
	}
}

// ciReleaseSpy returns the fixture's CI watch spy so a test can change the
// watch mode or records before the request.
func ciReleaseSpy(t *testing.T, s *Server) *consoleSpy {
	t.Helper()
	spy, ok := s.autonomousMgr.(*consoleSpy)
	if !ok {
		t.Fatalf("fixture autonomous manager is not the console spy")
	}
	return spy
}

func TestDashboardConsoleCIReleaseWatchOffKeepsReleaseAndShipState(t *testing.T) {
	s := consoleFixtureServer(t)
	s.latestVersion = func() (string, error) { return "99.0.0", nil }
	ciReleaseSpy(t, s).triage[0].Notes = []autonomous.TriageNote{
		{At: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), Source: "ship_step", Text: "pushed the release commit"},
	}
	_, resp := getConsole(t, s)

	rel := resp.CIRelase.Release
	if !resp.CIRelase.Available || rel == nil {
		t.Fatalf("ci_release: want an available section with release detail, got %+v", resp.CIRelase)
	}
	if !rel.WatchOff || rel.WatchMode != "off" {
		t.Errorf("watch: want off, got mode=%q watch_off=%v", rel.WatchMode, rel.WatchOff)
	}
	if rel.WatchLink != consoleAnchorSettings {
		t.Errorf("watch_link: got %q, want %q (the next action goes to settings)", rel.WatchLink, consoleAnchorSettings)
	}
	if len(rel.FailingRuns) != 0 {
		t.Errorf("failing_runs: a watch that is off must not list runs, got %d", len(rel.FailingRuns))
	}
	if rel.CurrentVersion != Version {
		t.Errorf("current_version: got %q, want %q", rel.CurrentVersion, Version)
	}
	if rel.ReleaseState != consoleReleaseAvailable || rel.LatestRelease != "99.0.0" {
		t.Errorf("release: got state=%q latest=%q, want update_available 99.0.0", rel.ReleaseState, rel.LatestRelease)
	}
	want := autonomous.BuildShipStatus(nil, "", "").Policy
	if rel.ShipPush != want.Push || rel.ShipTag != want.Tag || rel.ShipCIWatch != want.CIWatch {
		t.Errorf("ship policy: got push=%q tag=%q ci_watch=%q, want %q %q %q",
			rel.ShipPush, rel.ShipTag, rel.ShipCIWatch, want.Push, want.Tag, want.CIWatch)
	}
	if rel.ShipLastNote != "pushed the release commit" {
		t.Errorf("ship_last_note: got %q", rel.ShipLastNote)
	}
	if !strings.Contains(resp.CIRelase.Summary, "ship: push") {
		t.Errorf("summary: %q (want the ship policy line)", resp.CIRelase.Summary)
	}
}

func TestDashboardConsoleCIReleaseWatchOnListsFailingRuns(t *testing.T) {
	s := consoleFixtureServer(t)
	spy := ciReleaseSpy(t, s)
	spy.ci.Mode = "shadow"
	spy.ci.Records[1].Repo = "repo-a"
	spy.ci.Records[1].Workflow = "ci.yml"
	spy.ci.Records[1].RunID = "123"
	spy.ci.Records[1].RunURL = "https://ci.example/runs/123"
	_, resp := getConsole(t, s)

	rel := resp.CIRelase.Release
	if rel == nil || rel.WatchOff {
		t.Fatalf("watch on: want release detail with watch_off=false, got %+v", rel)
	}
	if len(rel.FailingRuns) != 1 {
		t.Fatalf("failing_runs: want 1 failing run, got %d", len(rel.FailingRuns))
	}
	run := rel.FailingRuns[0]
	if run.Repo != "repo-a" || run.RunID != "123" || run.Status != "failing" || run.RunURL != "https://ci.example/runs/123" {
		t.Errorf("failing run: got %+v", run)
	}
	if rel.ReleaseState != consoleReleaseNotWired {
		t.Errorf("release_state: with no version check wired got %q, want %q", rel.ReleaseState, consoleReleaseNotWired)
	}
}

func TestDashboardConsoleCIReleaseVersionCheckStates(t *testing.T) {
	cases := []struct {
		name   string
		fn     func() (string, error)
		state  string
		latest string
	}{
		{"up to date when no newer tag", func() (string, error) { return "", nil }, consoleReleaseUpToDate, ""},
		{"check failed", func() (string, error) { return "", fmt.Errorf("offline") }, consoleReleaseCheckFail, ""},
		{"update available", func() (string, error) { return "99.0.0", nil }, consoleReleaseAvailable, "99.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := consoleFixtureServer(t)
			s.latestVersion = tc.fn
			_, resp := getConsole(t, s)
			if resp.CIRelase.Release.ReleaseState != tc.state {
				t.Errorf("release_state: got %q, want %q", resp.CIRelase.Release.ReleaseState, tc.state)
			}
			if resp.CIRelase.Release.LatestRelease != tc.latest {
				t.Errorf("latest_release: got %q, want %q", resp.CIRelase.Release.LatestRelease, tc.latest)
			}
		})
	}
}

func TestDashboardConsoleCIReleaseOmittedWhenSectionHidden(t *testing.T) {
	s := consoleFixtureServer(t)
	off := false
	s.cfg.Dashboard.Console.ShowCIRelease = &off
	_, resp := getConsole(t, s)
	if resp.CIRelase.Available || resp.CIRelase.Release != nil {
		t.Errorf("ci_release hidden by its switch: want no detail, got %+v", resp.CIRelase)
	}
}

func TestConsoleFailingRunsNewestFirstAndCapped(t *testing.T) {
	base := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	var recs []*autonomous.RepoCIWatchRecord
	for i := 0; i < 7; i++ {
		recs = append(recs, &autonomous.RepoCIWatchRecord{
			RunID: fmt.Sprintf("r%d", i), Status: "hung", UpdatedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}
	recs = append(recs, &autonomous.RepoCIWatchRecord{RunID: "green", Status: "green", UpdatedAt: base.Add(100 * time.Hour)}, nil)
	got := consoleFailingRuns(recs)
	if len(got) != consoleFailingRunLimit {
		t.Fatalf("want %d runs (the cap), got %d", consoleFailingRunLimit, len(got))
	}
	if got[0].RunID != "r6" || got[len(got)-1].RunID != "r2" {
		t.Errorf("order: want newest first r6..r2, got %s..%s", got[0].RunID, got[len(got)-1].RunID)
	}
	if n := len(consoleFailingRuns(nil)); n != 0 {
		t.Errorf("no records: want 0 runs, got %d", n)
	}
}
