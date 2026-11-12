package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func progressTestMetrics() autonomous.ProgressMetrics {
	low, high := 3.0, 6.5
	return autonomous.ProgressMetrics{
		Scope: "all", CalendarDays: 10, ActiveDays: 8, Lanes: 5,
		Groups: map[string]autonomous.ProgressGroup{
			"feature": {PRDs: 7, Done: 4, Tasks: 21, TasksDone: 12, TasksLeft: 9},
			"bug":     {PRDs: 2, Done: 2, Tasks: 4, TasksDone: 4},
			"other":   {},
		},
		Totals:     autonomous.ProgressGroup{PRDs: 9, Done: 6, Tasks: 25, TasksDone: 16, TasksLeft: 9},
		Remaining:  autonomous.RemainingWork{Combined: autonomous.RemainingCombined{PRDs: 4, Tasks: 17}},
		Completion: autonomous.CompletionRange{LowDays: &low, HighDays: &high},
		UnfiledRows: []autonomous.BacklogRow{
			{Item: "AB42 Write the migration guide"},
		},
		BacklogAvailable: true,
	}
}

// progressServer answers both progress routes and records the last query.
func progressServer(t *testing.T, query *string, trend string) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(progressTestMetrics())
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*query = r.URL.RawQuery
		switch r.URL.Path {
		case "/api/autonomous/progress":
			_, _ = w.Write(body)
		case "/api/autonomous/progress/trend":
			_, _ = w.Write([]byte(trend))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestProgressParse(t *testing.T) {
	for in, want := range map[string]string{
		"progress":                         "",
		"Progress":                         "",
		"progress tag=t1 since=2026-10-01": "tag=t1 since=2026-10-01",
		"progress trend":                   "trend",
		"progress trend 14":                "trend 14",
		"progress   automation=a1":         "automation=a1",
	} {
		cmd := Parse(in)
		if cmd.Type != CmdProgress || cmd.Text != want {
			t.Errorf("Parse(%q) = {%s %q}, want {progress %q}", in, cmd.Type, cmd.Text, want)
		}
	}
	if Parse("progressive").Type == CmdProgress {
		t.Error("a longer word must not parse as the progress verb")
	}
}

func TestProgressHelpListsVerbs(t *testing.T) {
	help := HelpText("h")
	for _, want := range []string{"progress [automation=<id>]", "progress trend [days]"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestProgressCommVerbsMatchParse(t *testing.T) {
	for _, v := range ProgressCommVerbs() {
		if Parse(v).Type != CmdProgress {
			t.Errorf("comm verb %q does not parse as the progress command", v)
		}
	}
}

func TestProgressCommReplyIsTheRenderedText(t *testing.T) {
	var query string
	srv := progressServer(t, &query, `{}`)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleProgress("automation=a1 tag=t1 since=2026-10-01 until=2026-10-20") })

	want := autonomous.RenderProgressText(progressTestMetrics())
	for _, line := range strings.Split(strings.TrimRight(want, "\n"), "\n") {
		if !strings.Contains(got, line) {
			t.Errorf("reply lacks the line %q\nreply:\n%s", line, got)
		}
	}
	for _, k := range []string{"automation_id=a1", "tag=t1", "since=2026-10-01", "until=2026-10-20"} {
		if !strings.Contains(query, k) {
			t.Errorf("query %q lacks %s", query, k)
		}
	}
	if strings.Contains(got, "AB42") {
		t.Errorf("tracker token reached the reply:\n%s", got)
	}
}

func TestProgressCommRejectsUnknownArgument(t *testing.T) {
	var query string
	srv := progressServer(t, &query, `{}`)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleProgress("colour=blue") })
	if !strings.Contains(got, `unknown argument "colour=blue"`) || !strings.Contains(got, "usage: progress") {
		t.Errorf("reply = %q", got)
	}
	if query != "" {
		t.Errorf("a bad argument must not reach the daemon, query = %q", query)
	}
}

func TestProgressCommTrend(t *testing.T) {
	var query string
	trend := `{"days":14,"direction":"converging","points":[{"date":"2026-10-22","reason":"daily","completion_low_days":4,"completion_high_days":6,"known_tasks":9,"estimated_tasks":3,"throughput_3d":1.5,"utilization":0.4,"completed_prds":6}]}`
	srv := progressServer(t, &query, trend)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleProgress("trend 14") })
	if query != "days=14" {
		t.Errorf("query = %q, want days=14", query)
	}
	for _, want := range []string{"last 14 days: completion range converging", "2026-10-22", "daily"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	bad := captureSends(r, func() { r.handleProgress("trend soon") })
	if !strings.Contains(bad, "days must be a whole number") {
		t.Errorf("bad days reply = %q", bad)
	}
}

func TestProgressCommReportsDaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request: since: not a date", http.StatusBadRequest)
	}))
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleProgress("since=soon") })
	if !strings.Contains(got, "progress failed") || !strings.Contains(got, "HTTP 400") {
		t.Errorf("reply = %q", got)
	}
}

// groupedProgressServer answers the progress route with group totals.
func groupedProgressServer(t *testing.T, query *string) *httptest.Server {
	t.Helper()
	m := progressTestMetrics()
	m.Completion.SampleRecords, m.Completion.SampleTasks = 6, 16
	body, err := json.Marshal(map[string]any{
		"scope": m.Scope, "totals": m.Totals, "groups": m.Groups, "completion": m.Completion,
		"group": "project",
		"group_totals": map[string]autonomous.ProgressGroup{
			"(none)": {PRDs: 3, Done: 1, Tasks: 5, TasksDone: 2, TasksLeft: 3},
			"zeta":   {PRDs: 2, Done: 2, Tasks: 4, TasksDone: 4},
			"alpha":  {PRDs: 4, Done: 3, Running: 1, Tasks: 9, TasksDone: 7, TasksLeft: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*query = r.URL.RawQuery
		_, _ = w.Write(body)
	}))
}

func TestProgressCommGroupedTotals(t *testing.T) {
	var query string
	srv := groupedProgressServer(t, &query)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleProgress("group=project") })
	if !strings.Contains(query, "group=project") {
		t.Errorf("query %q lacks group=project", query)
	}
	for _, want := range []string{"Totals by project:", "alpha", "zeta", "(none)", "estimate sample: 6 finished Automata, 16 completed tasks"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q\n%s", want, got)
		}
	}
	a, z, n := strings.Index(got, "alpha"), strings.Index(got, "zeta"), strings.Index(got, "(none)")
	if a >= z || z >= n {
		t.Errorf("rows must sort by name with (none) last\n%s", got)
	}
}

func TestProgressCommProjectKeepsOneRow(t *testing.T) {
	var query string
	srv := groupedProgressServer(t, &query)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleProgress("project=(none)") })
	if !strings.Contains(query, "group=project") {
		t.Errorf("project= must imply group=project, query %q", query)
	}
	if strings.Contains(query, "project=") && strings.Contains(strings.ReplaceAll(query, "group=project", ""), "project=") {
		t.Errorf("project must be filtered here, not sent: %q", query)
	}
	if !strings.Contains(got, "(none)") || strings.Contains(got, "alpha") || strings.Contains(got, "zeta") {
		t.Errorf("only the (none) row expected\n%s", got)
	}
	got = captureSends(r, func() { r.handleProgress("project=missing") })
	if !strings.Contains(got, `no Automata for project "missing"`) {
		t.Errorf("reply = %q", got)
	}
}

func TestProgressCommAutomationAndAutomatonAxes(t *testing.T) {
	var query string
	srv := groupedProgressServer(t, &query)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleProgress("group=automation") })
	if !strings.Contains(query, "group=automation") || !strings.Contains(got, "Totals by automation:") {
		t.Errorf("query %q reply %q", query, got)
	}
	got = captureSends(r, func() { r.handleProgress("automaton=zz") })
	if !strings.Contains(query, "group=automaton") || !strings.Contains(got, `no Automata for automaton "zz"`) {
		t.Errorf("query %q reply %q", query, got)
	}
}

func TestProgressCommGroupArgumentErrors(t *testing.T) {
	var query string
	srv := groupedProgressServer(t, &query)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	for _, in := range []string{"group=colour", "group=automaton project=a", "project=a automaton=b", "group=", "project="} {
		query = ""
		got := captureSends(r, func() { r.handleProgress(in) })
		if !strings.Contains(got, "usage: progress") || query != "" {
			t.Errorf("%q: reply %q query %q", in, got, query)
		}
	}
}
