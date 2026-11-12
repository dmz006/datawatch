// Cost outcomes REST tests: GET /api/cost/tool-usage and GET /api/cost/failures.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/costreport"
)

func getToolUsage(t *testing.T, s *Server, query string) (*httptest.ResponseRecorder, costreport.ToolUsage) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cost/tool-usage"+query, nil)
	rr := httptest.NewRecorder()
	s.handleCostToolUsage(rr, req)
	var out costreport.ToolUsage
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rr, out
}

func getCostFailures(t *testing.T, s *Server, query string) (*httptest.ResponseRecorder, costreport.Failures) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cost/failures"+query, nil)
	rr := httptest.NewRecorder()
	s.handleCostFailures(rr, req)
	var out costreport.Failures
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rr, out
}

func toolLedger(now time.Time) fakeSpendLedger {
	l := seriesLedger(now)
	for i := range l {
		l[i].ToolCalls = int64(10 * (i + 1))
	}
	return l
}

func TestCostToolUsage_DefaultsGroupByAutomaton(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = toolLedger(time.Now())
	rr, got := getToolUsage(t, s, "?window=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}
	if got.GroupBy != costreport.SeriesByAutomaton || got.Bucket != costreport.SeriesBucketHour {
		t.Fatalf("group_by=%q bucket=%q", got.GroupBy, got.Bucket)
	}
	byKey := map[string]costreport.ToolUsageGroup{}
	for _, g := range got.Groups {
		byKey[g.Key] = g
	}
	// s1 (10) and s2 (20) belong to a1, s3 (30) to a2, s4 (40) to nobody; the
	// 10-day-old entry is outside the window.
	if byKey["a1"].ToolCalls != 30 || byKey["a2"].ToolCalls != 30 || byKey[costreport.UnattributedKey].ToolCalls != 40 || len(got.Groups) != 3 {
		t.Errorf("groups wrong: %+v", got.Groups)
	}
	if got.TotalToolCalls != 100 {
		t.Errorf("total = %d", got.TotalToolCalls)
	}
	if !strings.Contains(got.ToolNamesNote, "not which tool") {
		t.Errorf("payload must say there is no per-tool name: %q", got.ToolNamesNote)
	}
}

func TestCostToolUsage_GroupByModelAndDayBucket(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = toolLedger(time.Now())
	rr, got := getToolUsage(t, s, "?group_by=model&window=3d&bucket=day")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got.GroupBy != costreport.SeriesByModel || got.Bucket != costreport.SeriesBucketDay {
		t.Fatalf("group_by=%q bucket=%q", got.GroupBy, got.Bucket)
	}
	byKey := map[string]int{}
	for _, g := range got.Groups {
		byKey[g.Key] = g.ToolCalls
	}
	if byKey["aider"] != 30 || byKey["ollama"] != 70 {
		t.Errorf("model groups wrong: %+v", byKey)
	}
}

func TestCostToolUsage_GroupByProjectUsesAutomatonProject(t *testing.T) {
	s := bl90Server(t)
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd, err := mgr.CreatePRD("project work", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.ProjectDir = "/work/alpha" // the store derives system tags itself on save
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	s.SetAutonomousAPI(newTestAPI(mgr))
	now := time.Now()
	s.costReportLedger = fakeSpendLedger{
		{SessionID: "s1", AutomatonID: prd.ID, StartedAt: now.Add(-time.Hour), ToolCalls: 9},
		{SessionID: "s2", AutomatonID: "stranger", StartedAt: now.Add(-time.Hour), ToolCalls: 4},
	}
	rr, got := getToolUsage(t, s, "?group_by=project&window=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(got.Groups) != 2 || got.Groups[0].Key != "alpha" || got.Groups[0].ToolCalls != 9 ||
		got.Groups[1].Key != costreport.UnattributedKey || got.Groups[1].ToolCalls != 4 {
		t.Fatalf("project groups wrong: %+v", got.Groups)
	}
}

func TestCostToolUsage_EmptyAndNoLedger(t *testing.T) {
	s := bl90Server(t)
	rr, got := getToolUsage(t, s, "?window=24h")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(got.Groups) != 0 || len(got.Buckets) == 0 {
		t.Fatalf("want no groups and an x axis, got %+v", got)
	}
	if !strings.Contains(rr.Body.String(), `"groups":[]`) || !strings.Contains(rr.Body.String(), `"tool_names_note"`) {
		t.Errorf("empty payload shape wrong: %s", rr.Body.String())
	}
}

func TestCostToolUsage_BadParams(t *testing.T) {
	s := bl90Server(t)
	for name, q := range map[string]string{
		"group_by":       "?group_by=week",
		"window":         "?window=soon",
		"window zero":    "?window=0h",
		"bucket":         "?bucket=minute",
		"bucket auto":    "?bucket=auto",
		"too many hours": "?window=90d&bucket=hour",
	} {
		rr, _ := getToolUsage(t, s, q)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d body=%s", name, rr.Code, rr.Body.String())
		}
	}
}

func TestCostToolUsage_MethodAndManager(t *testing.T) {
	s := bl90Server(t)
	rr := httptest.NewRecorder()
	s.handleCostToolUsage(rr, httptest.NewRequest(http.MethodPost, "/api/cost/tool-usage", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
	s.manager = nil
	rr, _ = getToolUsage(t, s, "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rr.Code)
	}
}

// ---- failures ----

// failureFixture builds three Automata with attempt records: the first in the
// "alpha" project and a member of the "nightly" Automation (through the
// member list only), the second in the same project with AutomationID set,
// and a standalone Automaton in a project of its own.
type failureFixture struct {
	server                *Server
	one, two, loose       *autonomous.PRD
	automationID          string
	thisWeek, lastWeekDay time.Time
}

func newFailureFixture(t *testing.T) failureFixture {
	t.Helper()
	s := bl90Server(t)
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(spec string) *autonomous.PRD {
		p, err := mgr.CreatePRD(spec, t.TempDir(), "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	f := failureFixture{server: s, one: mk("first alpha work"), two: mk("second alpha work"), loose: mk("loose work")}
	f.thisWeek = time.Now().UTC().Add(-time.Hour)
	f.lastWeekDay = f.thisWeek.AddDate(0, 0, -7)

	auto, err := mgr.CreateAutomation(autonomous.AutomationInput{
		Name:          "nightly",
		EntryMemberID: "m1",
		Members:       []autonomous.AutomationMember{{ID: "m1", PRDID: f.one.ID}},
	}, "test")
	if err != nil {
		t.Skipf("cannot build an Automation with a member here: %v", err)
	}
	f.automationID = auto.ID

	attempt := func(n int, at time.Time, outcome, cat, sum string) autonomous.TaskAttempt {
		return autonomous.TaskAttempt{Number: n, StartedAt: at.Add(-time.Minute), EndedAt: at, Outcome: outcome, FailureCategory: cat, FailureSummary: sum}
	}
	f.one.ProjectDir = "/work/alpha"
	f.one.Title = "First alpha"
	f.one.Story = []autonomous.Story{{ID: "st1", Tasks: []autonomous.Task{
		{ID: "t1", RetryCount: 2, Attempts: []autonomous.TaskAttempt{
			attempt(1, f.thisWeek, "failed", "infra", "tmux session vanished"),
			attempt(2, f.lastWeekDay, "failed", "infra", "tmux session vanished"),
		}},
		{ID: "t2", RetryCount: 1, Attempts: []autonomous.TaskAttempt{
			attempt(1, f.lastWeekDay, "stalled", "content", "tests still failing"),
		}},
	}}}
	f.two.ProjectDir = "/work/alpha"
	f.two.AutomationID = auto.ID
	f.two.Story = []autonomous.Story{{ID: "st1", Tasks: []autonomous.Task{
		// an attempt that never recorded an end falls back to its start
		{ID: "t1", RetryCount: 0, Attempts: []autonomous.TaskAttempt{
			{Number: 1, StartedAt: f.thisWeek, Outcome: "cancelled"},
		}},
	}}}
	f.loose.Story = []autonomous.Story{{ID: "st1", Tasks: []autonomous.Task{
		{ID: "t1", RetryCount: 3, Attempts: []autonomous.TaskAttempt{
			attempt(1, f.thisWeek, "failed", "content", "wrong output"),
			{Number: 2, Outcome: "failed", FailureCategory: "content"}, // no time at all
		}},
	}}}
	for _, p := range []*autonomous.PRD{f.one, f.two, f.loose} {
		if err := mgr.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	s.SetAutonomousAPI(newTestAPI(mgr))
	return f
}

func failureGroupOf(t *testing.T, f costreport.Failures, key string) costreport.FailureGroup {
	t.Helper()
	for _, g := range f.Groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("group %q missing: %+v", key, f.Groups)
	return costreport.FailureGroup{}
}

func TestCostFailures_ByProject(t *testing.T) {
	fx := newFailureFixture(t)
	rr, got := getCostFailures(t, fx.server, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}
	if got.GroupBy != costreport.FailuresByProject {
		t.Fatalf("group_by = %q", got.GroupBy)
	}
	alpha := failureGroupOf(t, got, "alpha")
	// two Automata in one project directory share one line
	if alpha.Attempts != 4 || alpha.Tasks != 3 || alpha.Retries != 3 {
		t.Fatalf("alpha = %+v", alpha)
	}
	cats := map[string]int{}
	for _, c := range alpha.Categories {
		cats[c.Category] = c.Attempts
	}
	if cats["infra"] != 2 || cats["content"] != 1 || cats[costreport.UncategorizedFailure] != 1 {
		t.Errorf("alpha categories = %+v", alpha.Categories)
	}
	if len(alpha.TopSummaries) == 0 || alpha.TopSummaries[0].Summary != "tmux session vanished" || alpha.TopSummaries[0].Attempts != 2 {
		t.Errorf("top summaries = %+v", alpha.TopSummaries)
	}
	// the standalone Automaton's project is its own directory's name
	if g := failureGroupOf(t, got, automatonProject(fx.loose)); g.Attempts != 1 || g.Key == "alpha" {
		t.Errorf("standalone Automaton's project line = %+v", g)
	}
	if got.UndatedAttempts != 1 {
		t.Errorf("undated = %d", got.UndatedAttempts)
	}
	if got.TotalAttempts != 5 || got.RetryNote == "" {
		t.Errorf("totals = %d, note %q", got.TotalAttempts, got.RetryNote)
	}
	if len(alpha.Weeks) != len(got.Weeks) {
		t.Errorf("weeks %d != %d", len(alpha.Weeks), len(got.Weeks))
	}
}

func TestCostFailures_ByAutomationUsesMemberListAndLabel(t *testing.T) {
	fx := newFailureFixture(t)
	rr, got := getCostFailures(t, fx.server, "?group_by=automation&window=4w")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	// first is a member only through the Automation's member list, second
	// through its own AutomationID; both land on the one Automation line.
	g := failureGroupOf(t, got, fx.automationID)
	if g.Label != "nightly" || g.Attempts != 4 || g.Tasks != 3 {
		t.Errorf("automation line = %+v", g)
	}
	if u := failureGroupOf(t, got, costreport.UnattributedKey); u.Attempts != 1 {
		t.Errorf("standalone Automaton = %+v", u)
	}
}

func TestCostFailures_ByAutomaton(t *testing.T) {
	fx := newFailureFixture(t)
	rr, got := getCostFailures(t, fx.server, "?group_by=automaton")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(got.Groups) != 3 {
		t.Fatalf("groups = %+v", got.Groups)
	}
	one := failureGroupOf(t, got, fx.one.ID)
	if one.Label != "First alpha" || one.Attempts != 3 || one.Retries != 3 {
		t.Errorf("first = %+v", one)
	}
	if two := failureGroupOf(t, got, fx.two.ID); two.Attempts != 1 {
		t.Errorf("second = %+v", two)
	}
}

func TestCostFailures_WindowNarrowsTheCounts(t *testing.T) {
	fx := newFailureFixture(t)
	rr, got := getCostFailures(t, fx.server, "?window=1d")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	// last week's attempts fall outside one day
	if alpha := failureGroupOf(t, got, "alpha"); alpha.Attempts != 2 {
		t.Errorf("alpha in a 1d window = %+v", alpha)
	}
}

func TestCostFailures_EmptyStore(t *testing.T) {
	s := bl90Server(t)
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAutonomousAPI(newTestAPI(mgr))
	rr, got := getCostFailures(t, s, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(got.Groups) != 0 || len(got.Weeks) == 0 || !strings.Contains(rr.Body.String(), `"groups":[]`) {
		t.Errorf("want no groups and an x axis: %s", rr.Body.String())
	}
}

func TestCostFailures_BadParams(t *testing.T) {
	s := bl90Server(t)
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAutonomousAPI(newTestAPI(mgr))
	for name, q := range map[string]string{
		"group_by":    "?group_by=model",
		"window":      "?window=soon",
		"window zero": "?window=0d",
		"too wide":    "?window=4000w",
	} {
		rr, _ := getCostFailures(t, s, q)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d body=%s", name, rr.Code, rr.Body.String())
		}
	}
}

func TestCostFailures_MethodAndNoAutonomousManager(t *testing.T) {
	s := bl90Server(t)
	rr := httptest.NewRecorder()
	s.handleCostFailures(rr, httptest.NewRequest(http.MethodPost, "/api/cost/failures", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
	rr, _ = getCostFailures(t, s, "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 without an autonomous manager, got %d", rr.Code)
	}
}

func TestCostFailureRecords_NilManager(t *testing.T) {
	if got := costFailureRecords(nil); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestCostOutcomes_RoutesRegistered(t *testing.T) {
	src, err := os.ReadFile("server.go") // tests run in the package directory
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"/api/cost/tool-usage", api.handleCostToolUsage`,
		`"/api/cost/failures", api.handleCostFailures`,
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("route %s is not registered in server.go", want)
		}
	}
}
