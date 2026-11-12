// supervisor_loop_comm_test.go — the chat-verb surface for the supervisor
// loop: the jobs and runs reads render the shared supervisorloop lines,
// the runs filters pass through to the daemon's query, the run verb posts
// the job's run route, an unknown job surfaces the daemon's 404, and both
// verb spellings are dispatched by the parity table.
package router

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

const supervisorLoopCommJobsBody = `{"enabled":true,"jobs":[
  {"name":"draft_spec","kind":"mechanical","enabled":true,"last_outcome":"ok","last_run":"2026-11-01T10:00:00Z"},
  {"name":"choose_backend","kind":"model","style":"fast","backend":"claude","model":"sonnet","enabled":false,"last_outcome":"escalated"}]}`

const supervisorLoopCommRunsBody = `{"runs":[
  {"job":"draft_spec","outcome":"ok","result":"drafted spec v2","started":"2026-11-01T10:00:00Z","duration_ms":1200},
  {"job":"draft_spec","outcome":"escalated","validator_failures":["spec too short"],"retries":3,"escalations":1,"started":"2026-11-01T09:00:00Z"}]}`

// supervisorLoopCommLines runs the verb and returns the body lines after
// the "[h] " + label header (the label is the sub-verb, e.g. "jobs").
func supervisorLoopCommLines(t *testing.T, r *Router, text, sub string) []string {
	t.Helper()
	out := strings.Join(r.HandleTestMessage(text), "\n")
	header := "[h] autonomous supervisor-loop " + sub + "\n"
	i := strings.Index(out, header)
	if i == -1 {
		t.Fatalf("reply carries no %q header: %q", header, out)
	}
	return strings.Split(out[i+len(header):], "\n")
}

// supervisorLoopCLILines renders the same body with the renderer the CLI
// calls, as the expected lines for the comm reply.
func supervisorLoopCLILines(t *testing.T, jobs bool, body string) []string {
	t.Helper()
	var b strings.Builder
	if jobs {
		var parsed supervisorloop.JobsBody
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Fatalf("sample body: %v", err)
		}
		supervisorloop.RenderJobs(&b, &parsed)
	} else {
		var parsed supervisorloop.RunsBody
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Fatalf("sample body: %v", err)
		}
		supervisorloop.RenderRuns(&b, &parsed)
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

func TestSupervisorLoopComm_JobsVerbOutput(t *testing.T) {
	if !slices.Contains(SupervisorLoopCommVerbs(), "supervisor") {
		t.Fatalf("SupervisorLoopCommVerbs() = %v, must contain %q", SupervisorLoopCommVerbs(), "supervisor")
	}
	for _, verb := range SupervisorLoopCommVerbs() {
		c := Parse("autonomous " + verb)
		if c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}

	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorLoopCommJobsBody)
	r := setupPeerRouter(t, port)
	lines := supervisorLoopCommLines(t, r, "autonomous supervisor jobs", "jobs")
	if log.path != "/api/supervisor-loop/jobs" || log.query != "" {
		t.Errorf("path=%s query=%q; want the supervisor loop jobs route with no filters", log.path, log.query)
	}
	if log.calls != 1 {
		t.Errorf("the read is one call, got %d", log.calls)
	}

	// The reply is the CLI's lines, line for line.
	want := supervisorLoopCLILines(t, true, supervisorLoopCommJobsBody)
	if !slices.Equal(lines, want) {
		t.Errorf("comm lines differ from the CLI renderer:\n got: %q\nwant: %q", lines, want)
	}

	got := strings.Join(lines, "\n")
	for _, frag := range []string{
		"Supervisor loop on; 2 jobs.",
		"draft_spec: mechanical, enabled, last ok at 2026-11-01T10:00:00Z",
		"choose_backend: model, disabled, fast, claude, sonnet, last escalated",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("reply lacks %q:\n%s", frag, got)
		}
	}
	if strings.Contains(got, "PRD") {
		t.Errorf("the reply must be free of the word PRD: %q", got)
	}

	// The hyphen and underscore aliases answer the same read.
	for _, alias := range []string{"supervisor-loop", "supervisor_loop"} {
		_, port := laneFillCommDaemon(t, http.StatusOK, supervisorLoopCommJobsBody)
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous "+alias+" jobs"), " ")
		if !strings.Contains(got, "Supervisor loop on") {
			t.Errorf("alias %q must answer the read, got: %q", alias, got)
		}
	}
}

func TestSupervisorLoopComm_RunsFiltersPassThrough(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorLoopCommRunsBody)
	r := setupPeerRouter(t, port)
	lines := supervisorLoopCommLines(t, r, "autonomous supervisor runs job=draft_spec outcome=escalated limit=25", "runs")
	if log.path != "/api/supervisor-loop/runs" {
		t.Errorf("path=%s; want the supervisor loop runs route", log.path)
	}
	if log.query != "job=draft_spec&limit=25&outcome=escalated" {
		t.Errorf("query=%q; want the filters for the daemon", log.query)
	}
	want := supervisorLoopCLILines(t, false, supervisorLoopCommRunsBody)
	if !slices.Equal(lines, want) {
		t.Errorf("comm lines differ from the CLI renderer:\n got: %q\nwant: %q", lines, want)
	}

	// An unknown argument is refused, naming it, without reaching the daemon.
	log, port = laneFillCommDaemon(t, http.StatusOK, supervisorLoopCommRunsBody)
	r = setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor runs style=fast"), " ")
	if !strings.Contains(got, "unknown argument style=fast") || !strings.Contains(got, "usage:") {
		t.Errorf("an unknown argument must be refused naming it, with the usage; got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusal must not reach the daemon, got %d calls", log.calls)
	}
}

func TestSupervisorLoopComm_RunVerbs(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"job":"draft_spec","ok":true,"result":"drafted spec v2"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor run job=draft_spec input=draft candidates=backend-a,backend-b"), " ")
	if log.path != "/api/supervisor-loop/jobs/draft_spec/run" {
		t.Errorf("path=%s; want the job's run route", log.path)
	}
	if !strings.Contains(log.body, `"input":"draft"`) || !strings.Contains(log.body, `"backend-a"`) || !strings.Contains(log.body, `"backend-b"`) {
		t.Errorf("request body lost the input or candidates: %s", log.body)
	}
	if !strings.Contains(got, "job draft_spec: ok") || !strings.Contains(got, "drafted spec v2") {
		t.Errorf("reply lacks the run's result: %q", got)
	}

	// The job name is required.
	log, port = laneFillCommDaemon(t, http.StatusOK, `{}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous supervisor run input=hello"), " ")
	if !strings.Contains(got, "run needs job=NAME") || !strings.Contains(got, "usage:") {
		t.Errorf("a missing job must be refused with the usage; got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusal must not reach the daemon, got %d calls", log.calls)
	}

	// An unknown job surfaces the daemon's 404.
	_, port = laneFillCommDaemon(t, http.StatusNotFound, `{"error":"unknown job nosuchjob"}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous supervisor run job=nosuchjob"), " ")
	if !strings.Contains(got, "HTTP 404") || !strings.Contains(got, "nosuchjob") {
		t.Errorf("the daemon's unknown-job error must surface, got: %q", got)
	}
}

func TestSupervisorLoopComm_DaemonRefusalPassesThrough(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor jobs"), " ")
	if !strings.Contains(got, "HTTP 503") || !strings.Contains(got, "autonomous disabled") {
		t.Errorf("the daemon refusal must pass through, got: %q", got)
	}
}

// The parity table (sx2_parity.go) lists the verb in the autonomous help
// and dispatches both spellings.
func TestSupervisorLoopComm_ParityTableEntry(t *testing.T) {
	r := setupPeerRouter(t, 0)
	got := strings.Join(r.HandleTestMessage("autonomous"), " ")
	if !strings.Contains(got, "supervisor jobs") || !strings.Contains(got, "supervisor run job=NAME") {
		t.Errorf("the autonomous help must list the supervisor loop verbs; got:\n%s", got)
	}
	for _, verb := range SupervisorLoopCommVerbs() {
		_, port := laneFillCommDaemon(t, http.StatusOK, supervisorLoopCommJobsBody)
		r := setupPeerRouter(t, port)
		out := strings.Join(r.HandleTestMessage("autonomous "+verb+" jobs"), " ")
		if strings.Contains(out, "unknown") || !strings.Contains(out, "Supervisor loop on") {
			t.Errorf("verb %q must be dispatched by the parity table; got: %q", verb, out)
		}
	}
}

// The bare verbs answer without the autonomous prefix.
func TestSupervisorLoopCommBareVerbs(t *testing.T) {
	for _, text := range []string{"supervisor jobs", "supervisor-loop jobs", "supervisor_loop jobs"} {
		_, port := laneFillCommDaemon(t, http.StatusOK, supervisorLoopCommJobsBody)
		r := setupPeerRouter(t, port)
		out := strings.Join(r.HandleTestMessage(text), " ")
		if !strings.Contains(out, "Supervisor loop on") {
			t.Errorf("%q must answer the jobs read; got %q", text, out)
		}
	}
	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorLoopCommRunsBody)
	r := setupPeerRouter(t, port)
	_ = strings.Join(r.HandleTestMessage("supervisor runs job=draft_spec outcome=ok"), " ")
	if log.path != "/api/supervisor-loop/runs" || !strings.Contains(log.query, "job=draft_spec") || !strings.Contains(log.query, "outcome=ok") {
		t.Errorf("bare runs filter: path=%s query=%q", log.path, log.query)
	}
	_, port = laneFillCommDaemon(t, http.StatusNotFound, `{"error":"unknown job nosuchjob"}`)
	r = setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("supervisor run job=nosuchjob"), " ")
	if !strings.Contains(got, "nosuchjob") {
		t.Errorf("bare run of an unknown job must surface the error, got %q", got)
	}
}

// configure supervisor_loop.<key>=<value> goes through the generic configure path.
func TestSupervisorLoopConfigureVerb(t *testing.T) {
	r := setupPeerRouter(t, 0)
	var gotKey, gotVal string
	r.SetConfigureFunc(func(key, value string) error { gotKey, gotVal = key, value; return nil })
	r.HandleTestMessage("configure supervisor_loop.enabled=false")
	if gotKey != "supervisor_loop.enabled" || gotVal != "false" {
		t.Errorf("configure passed %q=%q", gotKey, gotVal)
	}
}

func TestSupervisorLoopComm_StatsVerb(t *testing.T) {
	body := `{"enabled":true,"jobs":11,"jobs_enabled":10,"runs":4,"runs_ok":2,"runs_validator_failed":1,"runs_escalated":1,"runs_error":0,"retries":3,"last_run_age_seconds":42}`
	log, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	lines := supervisorLoopCommLines(t, r, "supervisor stats", "stats")
	if log.path != "/api/supervisor-loop/stats" {
		t.Errorf("path=%s", log.path)
	}
	got := strings.Join(lines, "\n")
	for _, frag := range []string{"Supervisor loop on; 10 of 11 jobs enabled.", "Runs stored: 4 (ok 2, validator failed 1, escalated 1, error 0); retries used: 3.", "Last run 42 seconds ago."} {
		if !strings.Contains(got, frag) {
			t.Errorf("reply lacks %q:\n%s", frag, got)
		}
	}
}
