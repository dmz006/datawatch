package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func progressCLIMetrics() autonomous.ProgressMetrics {
	low, high := 2.0, 4.5
	return autonomous.ProgressMetrics{
		Scope: "all", CalendarDays: 6, ActiveDays: 5, Lanes: 5,
		Groups: map[string]autonomous.ProgressGroup{
			"feature": {PRDs: 3, Done: 1, Tasks: 9, TasksDone: 4, TasksLeft: 5},
			"bug":     {},
			"other":   {},
		},
		Totals:           autonomous.ProgressGroup{PRDs: 3, Done: 1, Tasks: 9, TasksDone: 4, TasksLeft: 5},
		Completion:       autonomous.CompletionRange{LowDays: &low, HighDays: &high},
		BacklogAvailable: true,
	}
}

// progressCLIServer answers both progress routes with fixed bodies and records
// each request.
func progressCLIServer(t *testing.T, metricsBody, trendBody []byte, gotURI *string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotURI = r.URL.RequestURI()
		switch r.URL.Path {
		case "/api/autonomous/progress":
			_, _ = w.Write(metricsBody)
		case "/api/autonomous/progress/trend":
			_, _ = w.Write(trendBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
}

func runProgressCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := newAutonomousCmd()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestProgressCLIRegistered(t *testing.T) {
	c := newAutonomousCmd()
	byName := map[string]bool{}
	for _, sub := range c.Commands() {
		byName[sub.Name()] = true
	}
	for _, name := range []string{"progress", "progress-trend"} {
		if !byName[name] {
			t.Errorf("autonomous has no %q subcommand", name)
		}
	}
	p := newAutonomousProgressCmd()
	for _, f := range []string{"json", "automation", "tag", "since", "until"} {
		if p.Flags().Lookup(f) == nil {
			t.Errorf("progress lacks --%s", f)
		}
	}
	tr := newAutonomousProgressTrendCmd()
	for _, f := range []string{"json", "days"} {
		if tr.Flags().Lookup(f) == nil {
			t.Errorf("progress-trend lacks --%s", f)
		}
	}
}

func TestProgressCLITextIsTheSharedRenderer(t *testing.T) {
	m := progressCLIMetrics()
	body, _ := json.Marshal(m)
	var uri string
	progressCLIServer(t, body, nil, &uri)

	out, err := runProgressCmd(t, "progress")
	if err != nil {
		t.Fatal(err)
	}
	if want := autonomous.RenderProgressText(m); out != want {
		t.Errorf("CLI text differs from RenderProgressText\n--- got ---\n%s--- want ---\n%s", out, want)
	}
	if uri != "/api/autonomous/progress" {
		t.Errorf("requested %q", uri)
	}
}

func TestProgressCLIJSONIsTheRESTBodyUntouched(t *testing.T) {
	// Deliberately odd spacing and key order: --json must not re-encode.
	const body = `{ "scope":"all",   "groups":{}, "extra_field":1 }`
	var uri string
	progressCLIServer(t, []byte(body), []byte(` {"days":7,"points":[],"direction":"unknown"} `), &uri)

	out, err := runProgressCmd(t, "progress", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if out != body {
		t.Errorf("--json output = %q, want the body %q", out, body)
	}
	out, err = runProgressCmd(t, "progress-trend", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if out != ` {"days":7,"points":[],"direction":"unknown"} ` {
		t.Errorf("trend --json output = %q", out)
	}
}

func TestProgressCLIFlagsBecomeQuery(t *testing.T) {
	body, _ := json.Marshal(progressCLIMetrics())
	var uri string
	progressCLIServer(t, body, []byte(`{"days":14,"points":[],"direction":"unknown"}`), &uri)

	if _, err := runProgressCmd(t, "progress", "--automation", "a1", "--tag", "t1", "--since", "2026-10-01", "--until", "2026-10-20"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"automation_id=a1", "tag_id=t1", "since=2026-10-01", "until=2026-10-20"} {
		if !strings.Contains(uri, want) {
			t.Errorf("request %q lacks %s", uri, want)
		}
	}
	if _, err := runProgressCmd(t, "progress"); err != nil {
		t.Fatal(err)
	}
	if uri != "/api/autonomous/progress" {
		t.Errorf("no flags should give no query, got %q", uri)
	}
	if _, err := runProgressCmd(t, "progress-trend", "--days", "14"); err != nil {
		t.Fatal(err)
	}
	if uri != "/api/autonomous/progress/trend?days=14" {
		t.Errorf("trend request = %q", uri)
	}
}

func TestProgressCLITrendText(t *testing.T) {
	trend := autonomous.ProgressTrendBody{Days: 30, Direction: "flat", Points: []autonomous.ProgressTrendPoint{
		{Date: "2026-10-22", Reason: "daily", KnownTasks: 5, CompletedPRDs: 2},
	}}
	body, _ := json.Marshal(trend)
	var uri string
	progressCLIServer(t, []byte(`{}`), body, &uri)
	out, err := runProgressCmd(t, "progress-trend")
	if err != nil {
		t.Fatal(err)
	}
	if want := autonomous.RenderTrendReply(30, "flat", trend.Points); out != want {
		t.Errorf("trend text\n--- got ---\n%s--- want ---\n%s", out, want)
	}
	if uri != "/api/autonomous/progress/trend" {
		t.Errorf("requested %q", uri)
	}
}

func TestProgressCLIReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request: since: not a date", http.StatusBadRequest)
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()
	t.Setenv("DATAWATCH_TOKEN", "test-token")

	_, err := runProgressCmd(t, "progress", "--since", "soon")
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error = %v, want one naming HTTP 400", err)
	}
}

func TestProgressCLIRejectsUnreadableBody(t *testing.T) {
	var uri string
	progressCLIServer(t, []byte(`<html>`), []byte(`<html>`), &uri)
	if _, err := runProgressCmd(t, "progress"); err == nil || !strings.Contains(err.Error(), "unreadable progress response") {
		t.Errorf("error = %v", err)
	}
	if _, err := runProgressCmd(t, "progress-trend"); err == nil || !strings.Contains(err.Error(), "unreadable progress trend response") {
		t.Errorf("error = %v", err)
	}
}
