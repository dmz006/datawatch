package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// dormantFeaturesCLIBody lists the seven off settings the registry reports.
const dormantFeaturesCLIBody = `{"features":[
 {"key":"autonomous.scan.fix_loop_enabled","name":"Scan fix loop","default":"off","current":"off","state":"off","purpose":"Opens a fix task when a scan fails.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.scan.rules_grader_enabled","name":"Scan rules grader","default":"off","current":"off","state":"off","purpose":"Grades a scan against the project rules.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.scope_breadth_enabled","name":"Scope breadth check","default":"off","current":"off","state":"off","purpose":"Marks a task too broad for the Automaton.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.default_quality_gates.enabled","name":"Default quality gates","default":"off","current":"off","state":"off","purpose":"Runs the test baseline before a task.","ever_exercised":true,"last_exercised":"2026-11-10T09:00:00Z","evidence":"decision log entry"},
 {"key":"autonomous.remote_reap_enabled","name":"Remote branch reap","default":"off","current":"off","state":"off","purpose":"Deletes merged Automaton branches from origin.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.notify.forwarding.enabled","name":"Notification forwarding","default":"off","current":"off","state":"off","purpose":"Forwards Automata events to federation peers.","ever_exercised":false,"evidence":"no record found"},
 {"key":"supervisor_loop.run_merge_until_conflict.enabled","name":"Supervisor loop merge step","default":"off","current":"off","state":"off","purpose":"Runs the merge pipeline until one conflicts.","ever_exercised":false,"evidence":"no record found; no merge shadow comparison exists"}
]}`

func dormantFeaturesDaemon(t *testing.T, status int, body string) (method, path *string) {
	t.Helper()
	var m, p string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, p = r.Method, r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return &m, &p
}

func runDormantFeaturesCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := &cobra.Command{Use: "autonomous", SilenceUsage: true, SilenceErrors: true}
	c.AddCommand(newAutonomousDormantFeaturesCmd())
	c.SetArgs(append([]string{"dormant-features"}, args...))
	out := &strings.Builder{}
	c.SetOut(out)
	err := c.Execute()
	return out.String(), err
}

func TestDormantFeaturesCLIRegistered(t *testing.T) {
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "dormant-features" {
			return
		}
	}
	t.Error("dormant-features is not registered under autonomous")
}

func TestDormantFeaturesCLITable(t *testing.T) {
	method, path := dormantFeaturesDaemon(t, 200, dormantFeaturesCLIBody)
	out, err := runDormantFeaturesCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if *method != http.MethodGet || *path != "/api/autonomous/dormant-features" {
		t.Errorf("request %s %s", *method, *path)
	}
	for _, frag := range []string{"KEY", "STATE", "DEFAULT", "PURPOSE", "EVER EXERCISED",
		"autonomous.scan.fix_loop_enabled", "autonomous.remote_reap_enabled", "Automata", "yes (last 2026-11-10T09:00:00Z)"} {
		if !strings.Contains(out, frag) {
			t.Errorf("output lacks %q:\n%s", frag, out)
		}
	}
	if n := strings.Count(out, "\n"); n != 8 {
		t.Errorf("want header plus seven rows, got %d lines:\n%s", n, out)
	}
}

func TestDormantFeaturesCLIJSONAndErrors(t *testing.T) {
	dormantFeaturesDaemon(t, 200, dormantFeaturesCLIBody)
	out, err := runDormantFeaturesCmd(t, "--json")
	if err != nil || out != dormantFeaturesCLIBody {
		t.Errorf("--json must pass the body untouched: err=%v", err)
	}
	dormantFeaturesDaemon(t, 200, `{"features":[]}`)
	out, err = runDormantFeaturesCmd(t)
	if err != nil || !strings.Contains(out, "No Automata setting is off or in shadow mode.") {
		t.Errorf("empty list: err=%v out=%q", err, out)
	}
	dormantFeaturesDaemon(t, 503, "autonomous is disabled")
	if _, err := runDormantFeaturesCmd(t); err == nil {
		t.Error("a server failure must return an error")
	}
	if _, err := runDormantFeaturesCmd(t, "extra"); err == nil {
		t.Error("an extra argument must be refused")
	}
}
