// cli_supervisor_loop_test.go — fakes the daemon and checks that the CLI
// reads the supervisor loop's jobs and runs routes (including the runs
// filters), renders the shared supervisorloop lines, passes --json through
// untouched, posts a run to the job's route, and fails on the daemon's
// unknown-job 404 and a 503.
package main

import (
	"encoding/json"
	"github.com/dmz006/datawatch/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// supervisorLoopDaemon starts a fake daemon answering with the given status
// and body, records the request, and points serverURL at it.
func supervisorLoopDaemon(t *testing.T, status int, body string) (reqMethod, reqPath func() string, reqQuery func() string, reqBody func() string) {
	t.Helper()
	var mu sync.Mutex
	method, path, query, raw := "", "", "", ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		method, path, query, raw = r.Method, r.URL.Path, r.URL.RawQuery, string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return func() string { mu.Lock(); defer mu.Unlock(); return method },
		func() string { mu.Lock(); defer mu.Unlock(); return path },
		func() string { mu.Lock(); defer mu.Unlock(); return query },
		func() string { mu.Lock(); defer mu.Unlock(); return raw }
}

func supervisorLoopRootCmd() *cobra.Command {
	c := &cobra.Command{Use: "datawatch"}
	c.AddCommand(newSupervisorLoopCmd())
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c
}

func runSupervisorLoopCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := supervisorLoopRootCmd()
	c.SetArgs(append([]string{"supervisor-loop"}, args...))
	out := &strings.Builder{}
	c.SetOut(out)
	err := c.Execute()
	return out.String(), err
}

const supervisorLoopJobsBody = `{"enabled":true,"jobs":[
  {"name":"draft_spec","kind":"mechanical","enabled":true,"last_outcome":"ok","last_run":"2026-11-01T10:00:00Z"},
  {"name":"choose_backend","kind":"model","style":"fast","backend":"claude","model":"sonnet","enabled":false,"last_outcome":"escalated"},
  {"name":"verify","kind":"mechanical","enabled":true,"last_outcome":"validator_failed"}]}`

const supervisorLoopRunsBody = `{"runs":[
  {"job":"draft_spec","outcome":"ok","result":"drafted spec v2","started":"2026-11-01T10:00:00Z","duration_ms":1200},
  {"job":"draft_spec","outcome":"escalated","validator_failures":["spec too short"],"retries":3,"escalations":1,"started":"2026-11-01T09:00:00Z"}]}`

func TestSupervisorLoopCLIRegisteredTopLevel(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "newSupervisorLoopCmd()") {
		t.Fatal("main.go does not register the top-level supervisor-loop command")
	}
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "supervisor-loop" {
			t.Fatal("supervisor-loop must be top-level, not nested under autonomous")
		}
	}
}

func TestSupervisorLoopCLIJobsText(t *testing.T) {
	getMethod, getPath, getQuery, _ := supervisorLoopDaemon(t, http.StatusOK, supervisorLoopJobsBody)
	s, err := runSupervisorLoopCmd(t, "jobs")
	if err != nil {
		t.Fatalf("execute: %v (output: %q)", err, s)
	}
	if m := getMethod(); m != http.MethodGet {
		t.Errorf("method = %q, want GET", m)
	}
	if p := getPath(); p != "/api/supervisor-loop/jobs" {
		t.Errorf("path = %q, want the supervisor loop jobs route", p)
	}
	if q := getQuery(); q != "" {
		t.Errorf("query = %q, want none", q)
	}
	for _, want := range []string{
		"Supervisor loop on; 3 jobs.",
		"draft_spec: mechanical, enabled, last ok at 2026-11-01T10:00:00Z",
		"choose_backend: model, disabled, fast, claude, sonnet, last escalated",
		"verify: mechanical, enabled, last validator_failed",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(strings.ToLower(s), "prd") {
		t.Errorf("operator output must not say PRD:\n%s", s)
	}
}

func TestSupervisorLoopCLIJobsJSONUntouched(t *testing.T) {
	supervisorLoopDaemon(t, http.StatusOK, supervisorLoopJobsBody)
	s, err := runSupervisorLoopCmd(t, "jobs", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// --json prints the body untouched; the sample body has no trailing
	// newline, so compare trimmed.
	if strings.TrimSpace(s) != strings.TrimSpace(supervisorLoopJobsBody) {
		t.Errorf("--json did not pass the body through untouched:\n%s", s)
	}
}

func TestSupervisorLoopCLIRunsFilter(t *testing.T) {
	getMethod, getPath, getQuery, _ := supervisorLoopDaemon(t, http.StatusOK, supervisorLoopRunsBody)
	s, err := runSupervisorLoopCmd(t, "runs", "--job", "draft_spec", "--outcome", "escalated", "--limit", "25")
	if err != nil {
		t.Fatalf("execute: %v (output: %q)", err, s)
	}
	if m := getMethod(); m != http.MethodGet {
		t.Errorf("method = %q, want GET", m)
	}
	if p := getPath(); p != "/api/supervisor-loop/runs" {
		t.Errorf("path = %q, want the supervisor loop runs route", p)
	}
	if q := getQuery(); q != "job=draft_spec&limit=25&outcome=escalated" {
		t.Errorf("query = %q, want the filters for the daemon", q)
	}
	for _, want := range []string{
		"1. draft_spec ok: drafted spec v2 (1200 ms) at 2026-11-01T10:00:00Z",
		"2. draft_spec escalated [validator: spec too short] [3 retries] [1 escalations] at 2026-11-01T09:00:00Z",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
}

func TestSupervisorLoopCLIRunsEmpty(t *testing.T) {
	supervisorLoopDaemon(t, http.StatusOK, `{"runs":[]}`)
	s, err := runSupervisorLoopCmd(t, "runs")
	if err != nil {
		t.Fatalf("execute: %v (output: %q)", err, s)
	}
	if !strings.Contains(s, "No run records.") {
		t.Errorf("empty state lacks the no-records line:\n%s", s)
	}
}

func TestSupervisorLoopCLIRunPostsTheJob(t *testing.T) {
	_, postPath, _, postBody := supervisorLoopDaemon(t, http.StatusOK, `{"job":"draft_spec","ok":true,"result":"drafted spec v2"}`)
	s, err := runSupervisorLoopCmd(t, "run", "draft_spec", "--input", "draft the spec", "--candidates", "backend-a", "--candidates", "backend-b")
	if err != nil {
		t.Fatalf("execute: %v (output: %q)", err, s)
	}
	if p := postPath(); p != "/api/supervisor-loop/jobs/draft_spec/run" {
		t.Errorf("path = %q, want the job's run route", p)
	}
	if b := postBody(); !strings.Contains(b, `"input":"draft the spec"`) || !strings.Contains(b, `"backend-a"`) || !strings.Contains(b, `"backend-b"`) {
		t.Errorf("request body lost the input or candidates: %s", b)
	}
	for _, want := range []string{"job draft_spec: ok", "drafted spec v2"} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
}

func TestSupervisorLoopCLIUnknownJobFails(t *testing.T) {
	_, postPath, _, _ := supervisorLoopDaemon(t, http.StatusNotFound, `{"error":"unknown job nosuchjob"}`)
	s, err := runSupervisorLoopCmd(t, "run", "nosuchjob")
	if err == nil {
		t.Fatalf("an unknown job must fail the command; output: %q", s)
	}
	if p := postPath(); p != "/api/supervisor-loop/jobs/nosuchjob/run" {
		t.Errorf("path = %q, want the unknown job's run route", p)
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "nosuchjob") {
		t.Errorf("error %q does not carry the status and the daemon's message", err)
	}
}

func TestSupervisorLoopCLIServerErrorExitCode(t *testing.T) {
	supervisorLoopDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	_, err := runSupervisorLoopCmd(t, "jobs")
	if err == nil {
		t.Fatal("a server error must fail the command (non-zero exit)")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "autonomous disabled") {
		t.Errorf("error %q does not carry the status and the server message", err)
	}
}

// `datawatch config set supervisor_loop.<key> <value>` has no key list of its
// own, so every key reaches the daemon as typed.
func TestSupervisorLoopCLIConfigSetKeys(t *testing.T) {
	cases := []struct {
		key, value string
		want       any
	}{
		{"supervisor_loop.enabled", "true", true},
		{"supervisor_loop.validator_retries", "4", float64(4)},
		{"supervisor_loop.run_retention", "200", float64(200)},
		{"supervisor_loop.archive_cancelled_after_minutes", "15", float64(15)},
	}
	for _, c := range cases {
		d := startInfraRetryConfigDaemon(t)
		if _, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		}); err != nil {
			t.Errorf("%s: %v", c.key, err)
			continue
		}
		if got := d.config[c.key]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: daemon stored %#v, want %#v", c.key, got, c.want)
		}
	}
}

// TestSupervisorLoopCLIArchiveGraceKey: `datawatch config set` sends the
// archive-grace key to the daemon as a JSON number, in both accepted key
// spellings, and shows the daemon's refusal text for a negative value.
func TestSupervisorLoopCLIArchiveGraceKey(t *testing.T) {
	for _, key := range []string{
		"supervisor_loop.archive_cancelled_after_minutes",
		"autonomous.supervisor_loop.archive_cancelled_after_minutes",
	} {
		for _, c := range []struct {
			value string
			want  float64
		}{{"15", 15}, {"0", 0}} {
			d := startInfraRetryConfigDaemon(t)
			out, err := captureEscalationStdout(t, func() error {
				return runConfigSet(&config.Config{}, key, c.value)
			})
			if err != nil {
				t.Errorf("%s=%s: %v", key, c.value, err)
				continue
			}
			if got := d.config[key]; got != c.want {
				t.Errorf("%s=%s: daemon stored %#v, want %v", key, c.value, got, c.want)
			}
			if !strings.Contains(out, "Set "+key+" = "+c.value) {
				t.Errorf("%s=%s: output %q lacks the Set confirmation", key, c.value, out)
			}
		}
	}

	// A negative value is refused by the daemon; the CLI carries its message.
	const refusal = "supervisor_loop.archive_cancelled_after_minutes: must be >= 0, got -5"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		http.Error(w, refusal, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	_, err := captureEscalationStdout(t, func() error {
		return runConfigSet(&config.Config{}, "supervisor_loop.archive_cancelled_after_minutes", "-5")
	})
	if err == nil || !strings.Contains(err.Error(), refusal) {
		t.Errorf("negative value: error %v does not carry %q", err, refusal)
	}
}

const supervisorWakeBodyJSON = `{"enabled":true,"kinds":["task.failed","prd.blocked"],"coalesce_seconds":45,"fallback_interval_minutes":10,"last_event":"0001-01-01T00:00:00Z","last_scheduled_check":"2026-11-01T10:00:00Z","event_count":0,"scheduled_count":3}`

func TestSupervisorWakeStatusText(t *testing.T) {
	method, path, _, _ := supervisorLoopDaemon(t, 200, supervisorWakeBodyJSON)
	out, err := runSupervisorLoopCmd(t, "wake")
	if err != nil {
		t.Fatal(err)
	}
	if method() != "GET" || path() != "/api/supervisor-loop/wake" {
		t.Errorf("request = %s %s", method(), path())
	}
	for _, want := range []string{"wake: on", "task.failed, prd.blocked", "45s", "10 min", "last event: never", "2026-11-01T10:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestSupervisorWakeSetPatchBody(t *testing.T) {
	var got map[string]any
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			gotMethod, gotPath = r.Method, r.URL.Path
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &got)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(supervisorWakeBodyJSON))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()
	if _, err := runSupervisorLoopCmd(t, "wake", "--enabled=true", "--kinds", "task.failed, prd.blocked", "--coalesce-seconds", "0"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "PUT" || gotPath != "/api/config" {
		t.Fatalf("patch request = %s %s", gotMethod, gotPath)
	}
	want := map[string]any{
		"supervisor_loop.wake.enabled":          true,
		"supervisor_loop.wake.kinds":            []any{"task.failed", "prd.blocked"},
		"supervisor_loop.wake.coalesce_seconds": float64(0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("patch = %#v, want %#v", got, want)
	}
}

func TestSupervisorWakeSetRefusalShown(t *testing.T) {
	supervisorLoopDaemon(t, 400, `unknown event kind "x"`)
	_, err := runSupervisorLoopCmd(t, "wake", "--kinds", "x")
	if err == nil || !strings.Contains(err.Error(), "unknown event kind") {
		t.Errorf("err = %v, want the daemon's refusal", err)
	}
}
