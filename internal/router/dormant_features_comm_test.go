package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// dormantFeaturesCommBody lists the seven off settings the registry reports.
const dormantFeaturesCommBody = `{"features":[
 {"key":"autonomous.scan.fix_loop_enabled","name":"Scan fix loop","default":"off","current":"off","state":"off","purpose":"Opens a fix task when a scan fails.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.scan.rules_grader_enabled","name":"Scan rules grader","default":"off","current":"off","state":"off","purpose":"Grades a scan against the project rules.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.scope_breadth_enabled","name":"Scope breadth check","default":"off","current":"off","state":"off","purpose":"Marks a task too broad for the Automaton.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.default_quality_gates.enabled","name":"Default quality gates","default":"off","current":"off","state":"off","purpose":"Runs the test baseline before a task.","ever_exercised":true,"last_exercised":"2026-11-10T09:00:00Z","evidence":"decision log entry"},
 {"key":"autonomous.remote_reap_enabled","name":"Remote branch reap","default":"off","current":"off","state":"off","purpose":"Deletes merged Automaton branches from origin.","ever_exercised":false,"evidence":"no record found"},
 {"key":"autonomous.notify.forwarding.enabled","name":"Notification forwarding","default":"off","current":"off","state":"off","purpose":"Forwards Automata events to federation peers.","ever_exercised":false,"evidence":"no record found"},
 {"key":"supervisor_loop.run_merge_until_conflict.enabled","name":"Supervisor loop merge step","default":"off","current":"off","state":"off","purpose":"Runs the merge pipeline until one conflicts.","ever_exercised":false,"evidence":"no record found; no merge shadow comparison exists"}
]}`

func TestDormantFeaturesCommVerbReadsRegistry(t *testing.T) {
	if !slices.Contains(DormantFeaturesCommVerbs(), "dormant-features") {
		t.Fatalf("DormantFeaturesCommVerbs() = %v", DormantFeaturesCommVerbs())
	}
	for _, verb := range []string{"dormant-features", "dormant_features"} {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v`, verb, c.Type)
		}
		log, port := laneFillCommDaemon(t, http.StatusOK, dormantFeaturesCommBody)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous "+verb), " ")
		if log.path != "/api/autonomous/dormant-features" || log.calls != 1 {
			t.Errorf("path=%s calls=%d", log.path, log.calls)
		}
		for _, want := range []string{"autonomous.scan.fix_loop_enabled", "Automata", "yes (last 2026-11-10T09:00:00Z)", "EVER EXERCISED"} {
			if !strings.Contains(got, want) {
				t.Errorf("reply lacks %q: %q", want, got)
			}
		}
	}
}

func TestDormantFeaturesCommEmptyErrorAndExtraArg(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, `{"features":[]}`)
	got := strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous dormant-features"), " ")
	if !strings.Contains(got, "No Automata setting is off or in shadow mode.") {
		t.Errorf("empty reply = %q", got)
	}
	_, port = laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous is disabled")
	got = strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous dormant-features"), " ")
	if !strings.Contains(got, "failed") {
		t.Errorf("error reply = %q", got)
	}
	_, port = laneFillCommDaemon(t, http.StatusOK, dormantFeaturesCommBody)
	got = strings.Join(setupPeerRouter(t, port).HandleTestMessage("autonomous dormant-features mode=on"), " ")
	if !strings.Contains(got, "unknown argument") || !strings.Contains(got, "usage: autonomous dormant-features") {
		t.Errorf("extra argument reply = %q", got)
	}
}
