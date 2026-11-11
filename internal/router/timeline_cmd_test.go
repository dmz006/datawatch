package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// timelineTestResult is the fixed timeline the fake daemon returns: one
// project, one Automaton with a projected finish, one stage, one completed
// member and one open blocked member.
func timelineTestResult() autonomous.TimelineResult {
	today := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	writeBars := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	writeBarsEnd := time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)
	ship := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	blocked := time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)
	projected := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	return autonomous.TimelineResult{
		Today: today,
		Projects: []autonomous.TimelineProject{
			{
				Project: "/home/dmz/repos/datawatch",
				Automata: []autonomous.TimelineAutomaton{
					{
						ID: "a1", Title: "Draw the timeline", Project: "/home/dmz/repos/datawatch",
						Kind: "feature", Status: "in_progress",
						ProjectedFinish: &projected,
						Stages: []autonomous.TimelineStage{
							{
								ID: "s1", Title: "Text bars", Status: "in_progress",
								Members: []autonomous.TimelineMember{
									{ID: "m1", Title: "Write the bars", Status: "completed", Start: &writeBars, End: &writeBarsEnd},
									{
										ID: "m2", Title: "Ship the channel", Status: "in_progress", Start: &ship,
										Intervals: []autonomous.TimelineInterval{
											{Kind: "blocked", Start: blocked, Reason: "waiting on the daemon"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		Counts: autonomous.TimelineCounts{Projects: 1, Automata: 1, Stages: 1, Members: 2, Blocked: 1},
	}
}

// timelineServer answers the timeline route with the fixed body and records
// the last query string.
func timelineServer(t *testing.T, query *string, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*query = r.URL.RawQuery
		if r.URL.Path != "/api/autonomous/timeline" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
}

func TestTimelineParse(t *testing.T) {
	for in, want := range map[string]string{
		"timeline":                          "",
		"Timeline":                          "",
		"timeline project=/repos/datawatch": "project=/repos/datawatch",
		"timeline automaton=a1 stage=s1 kind=bug": "automaton=a1 stage=s1 kind=bug",
		"timeline   tag=t1":                       "tag=t1",
	} {
		cmd := Parse(in)
		if cmd.Type != CmdTimeline || cmd.Text != want {
			t.Errorf("Parse(%q) = {%s %q}, want {timeline %q}", in, cmd.Type, cmd.Text, want)
		}
	}
	if Parse("timelines").Type == CmdTimeline {
		t.Error("a longer word must not parse as the timeline verb")
	}
	if Parse("kg timeline entity").Type == CmdTimeline {
		t.Error("kg timeline must stay inside the kg command")
	}
}

func TestTimelineHelpListsUsage(t *testing.T) {
	help := HelpText("h")
	if !strings.Contains(help, "timeline [project=<dir>] [automaton=<id>] [stage=<id>] [kind=<kind>] [tag=<id>]") {
		t.Errorf("help lacks the timeline usage line:\n%s", help)
	}
}

func TestTimelineCommVerbsMatchParse(t *testing.T) {
	if len(TimelineCommVerbs()) != 1 {
		t.Fatalf("comm verbs = %v", TimelineCommVerbs())
	}
	for _, v := range TimelineCommVerbs() {
		if Parse(v).Type != CmdTimeline {
			t.Errorf("comm verb %q does not parse as the timeline command", v)
		}
	}
}

func TestTimelineCommReplyIsTheSharedSummary(t *testing.T) {
	res := timelineTestResult()
	body, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var query string
	srv := timelineServer(t, &query, body)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() {
		r.handleTimeline("project=/repos/datawatch automaton=a1 stage=s1 kind=feature tag=t1")
	})

	if !strings.Contains(got, "Timeline:") {
		t.Errorf("reply lacks the Timeline: header:\n%s", got)
	}
	for _, want := range []string{
		"1 projects, 1 Automata, 1 stages, 2 members, 1 blocked",
		"Draw the timeline (in_progress): 2 members, 1 done, 1 blocked",
		"projected 2026-10-30",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	// The summary is the short form: no bar drawing.
	if strings.Contains(got, "span ") || strings.Contains(got, "Write the bars") {
		t.Errorf("summary reply must not carry the bars:\n%s", got)
	}
	if want := timelineQuery(); want != query {
		t.Errorf("query %q, want %q", query, want)
	}
}

// timelineQuery is the expected canonical query for the full fixture args.
func timelineQuery() string {
	return "automaton=a1&kind=feature&project=%2Frepos%2Fdatawatch&stage=s1&tag=t1"
}

func TestTimelineCommArgumentsAreCaseInsensitiveAndSkipEmpty(t *testing.T) {
	res := timelineTestResult()
	body, _ := json.Marshal(res)
	var query string
	srv := timelineServer(t, &query, body)
	defer srv.Close()
	r := loopbackRouter(t, srv)

	if got := captureSends(r, func() { r.handleTimeline("PROJECT=/repos/datawatch") }); !strings.Contains(got, "Timeline:") {
		t.Errorf("uppercase key rejected:\n%s", got)
	}
	if !strings.Contains(query, "project=%2Frepos%2Fdatawatch") {
		t.Errorf("query %q lacks the lowercased key", query)
	}

	query = ""
	got := captureSends(r, func() { r.handleTimeline("tag=") })
	if !strings.Contains(got, `unknown argument "tag="`) || !strings.Contains(got, "usage: timeline") {
		t.Errorf("empty value reply = %q", got)
	}
	if query != "" {
		t.Errorf("an empty value must not reach the daemon, query = %q", query)
	}
}

func TestTimelineCommRejectsUnknownArgument(t *testing.T) {
	res := timelineTestResult()
	body, _ := json.Marshal(res)
	var query string
	srv := timelineServer(t, &query, body)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleTimeline("colour=blue") })
	if !strings.Contains(got, `unknown argument "colour=blue"`) || !strings.Contains(got, "usage: timeline") {
		t.Errorf("reply = %q", got)
	}
	if query != "" {
		t.Errorf("a bad argument must not reach the daemon, query = %q", query)
	}
}

func TestTimelineCommReportsDaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request: automaton: unknown id", http.StatusBadRequest)
	}))
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleTimeline("automaton=nope") })
	if !strings.Contains(got, "timeline failed") || !strings.Contains(got, "HTTP 400") {
		t.Errorf("reply = %q", got)
	}
}

func TestTimelineCommReportsUnreadableBody(t *testing.T) {
	var query string
	srv := timelineServer(t, &query, []byte(`<html>`))
	defer srv.Close()
	r := loopbackRouter(t, srv)
	got := captureSends(r, func() { r.handleTimeline("") })
	if !strings.Contains(got, "timeline failed") {
		t.Errorf("reply = %q", got)
	}
}
