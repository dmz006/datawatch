package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// timelineCLIFixture is the timeline object the fake daemon returns: one
// project, one Automaton with a projected finish, one stage, one completed
// member and one open blocked member. All times are fixed.
func timelineCLIFixture() autonomous.TimelineResult {
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
				Project: "/home/<user>/repos/datawatch",
				Automata: []autonomous.TimelineAutomaton{
					{
						ID: "a1", Title: "Draw the timeline", Project: "/home/<user>/repos/datawatch",
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

// timelineCLIServer answers the timeline route with a fixed body and records
// each request.
func timelineCLIServer(t *testing.T, body []byte, gotURI *string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotURI = r.URL.RequestURI()
		if r.URL.Path != "/api/autonomous/timeline" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
}

func runTimelineCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := newAutonomousCmd()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append([]string{"timeline"}, args...))
	err := c.Execute()
	return out.String(), err
}

func TestTimelineCLIRegistered(t *testing.T) {
	c := newAutonomousCmd()
	byName := map[string]bool{}
	for _, sub := range c.Commands() {
		byName[sub.Name()] = true
	}
	if !byName["timeline"] {
		t.Fatal("autonomous has no timeline subcommand")
	}
	tl := newAutonomousTimelineCmd()
	for _, f := range []string{"json", "project", "automaton", "stage", "kind", "tag"} {
		if tl.Flags().Lookup(f) == nil {
			t.Errorf("timeline lacks --%s", f)
		}
	}
}

func TestTimelineCLITextIsTheSharedRenderer(t *testing.T) {
	res := timelineCLIFixture()
	body, _ := json.Marshal(res)
	var uri string
	timelineCLIServer(t, body, &uri)

	out, err := runTimelineCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if want := autonomous.RenderTimelineBars(res); out != want {
		t.Errorf("CLI text differs from RenderTimelineBars\n--- got ---\n%s--- want ---\n%s", out, want)
	}
	// The bar line for the blocked member carries the shared markers.
	if !strings.Contains(out, "Write the bars") || !strings.Contains(out, "Ship the channel") {
		t.Errorf("member lines missing:\n%s", out)
	}
	if !strings.Contains(out, "X") || !strings.Contains(out, "T") || !strings.Contains(out, "P") {
		t.Errorf("markers X/T/P missing:\n%s", out)
	}
	if !strings.Contains(out, "1 projects, 1 Automata, 1 stages, 2 members, 1 blocked") {
		t.Errorf("counts line missing:\n%s", out)
	}
	if uri != "/api/autonomous/timeline" {
		t.Errorf("requested %q", uri)
	}
}

func TestTimelineCLIDrawsTheEmptyChart(t *testing.T) {
	var uri string
	timelineCLIServer(t, []byte(`{"today":"2026-10-23T12:00:00Z","projects":[],"counts":{"projects":0,"automata":0,"stages":0,"members":0,"blocked":0}}`), &uri)
	out, err := runTimelineCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if want := "No Automata to draw.\n"; out != want {
		t.Errorf("empty chart = %q, want %q", out, want)
	}
}

func TestTimelineCLIJSONIsTheRESTBodyUntouched(t *testing.T) {
	// Deliberately odd spacing and key order: --json must not re-encode.
	const body = `{ "today":"2026-10-23",   "projects":[], "counts":{"projects":0} }`
	var uri string
	timelineCLIServer(t, []byte(body), &uri)

	out, err := runTimelineCmd(t, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if out != body {
		t.Errorf("--json output = %q, want the body %q", out, body)
	}
	if uri != "/api/autonomous/timeline" {
		t.Errorf("requested %q", uri)
	}
}

func TestTimelineCLIFlagsBecomeQuery(t *testing.T) {
	res := timelineCLIFixture()
	body, _ := json.Marshal(res)
	var uri string
	timelineCLIServer(t, body, &uri)

	if _, err := runTimelineCmd(t, "--project", "/home/<user>/repos/datawatch", "--automaton", "a1", "--stage", "s1", "--kind", "feature", "--tag", "t1"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"project=%2Fhome%2F%3Cuser%3E%2Frepos%2Fdatawatch", "automaton=a1", "stage=s1", "kind=feature", "tag=t1"} {
		if !strings.Contains(uri, want) {
			t.Errorf("request %q lacks %s", uri, want)
		}
	}
	if _, err := runTimelineCmd(t); err != nil {
		t.Fatal(err)
	}
	if uri != "/api/autonomous/timeline" {
		t.Errorf("no flags should give no query, got %q", uri)
	}
}

func TestTimelineCLIReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request: automaton: unknown id", http.StatusBadRequest)
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()
	t.Setenv("DATAWATCH_TOKEN", "test-token")

	_, err := runTimelineCmd(t, "--automaton", "nope")
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error = %v, want one naming HTTP 400", err)
	}
}

func TestTimelineCLIRejectsUnreadableBody(t *testing.T) {
	var uri string
	timelineCLIServer(t, []byte(`<html>`), &uri)
	if _, err := runTimelineCmd(t); err == nil {
		t.Error("unreadable body should fail")
	}
}
