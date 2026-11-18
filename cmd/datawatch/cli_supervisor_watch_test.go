// cli_supervisor_watch_test.go — fakes the daemon and checks that the CLI
// reads the supervisor watch route, renders the mode, last poll, one line per
// finding and the comparison summary, handles an empty state, passes --json
// through untouched and returns an error on a server failure.
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// supervisorWatchDaemon starts a fake daemon answering with the given status
// and body, records the request, and points serverURL at it.
func supervisorWatchDaemon(t *testing.T, status int, body string) (reqMethod func() string, reqPath func() string) {
	t.Helper()
	var mu sync.Mutex
	method, path := "", ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, path = r.Method, r.URL.Path
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
		func() string { mu.Lock(); defer mu.Unlock(); return path }
}

func supervisorWatchAutonomousCmd() *cobra.Command {
	c := &cobra.Command{Use: "autonomous"}
	c.AddCommand(newAutonomousSupervisorWatchCmd())
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c
}

const supervisorWatchBody = `{
  "mode": "shadow",
  "last_poll_at": "2026-10-29T10:15:00Z",
  "findings": [
    {
      "kind": "task_failed",
      "subject": "a1b2c3d4",
      "text": "a task in Automaton a1b2c3d4 failed: read the verifier verdict before retrying",
      "first_seen_at": "2026-10-29T09:40:00Z",
      "last_seen_at": "2026-10-29T10:15:00Z",
      "would_alert": true,
      "would_alert_at": "2026-10-29T10:10:00Z",
      "resolved": false
    },
    {
      "kind": "ci_red",
      "subject": "9001",
      "text": "CI run 9001 is red on main",
      "first_seen_at": "2026-10-29T08:00:00Z",
      "last_seen_at": "2026-10-29T09:00:00Z",
      "alerted_at": "2026-10-29T08:30:00Z",
      "resolved": false
    }
  ],
  "comparison": {
    "last": {
      "at": "2026-10-29T10:15:00Z",
      "watch_kinds": ["task_failed/a1b2c3d4"],
      "script_kinds": ["ci_red/9001"],
      "only_watch": ["task_failed/a1b2c3d4"],
      "only_script": ["ci_red/9001"],
      "agreed": false
    },
    "agreed_days": 3,
    "only_watch": ["task_failed/a1b2c3d4"],
    "only_script": ["ci_red/9001"]
  }
}`

func runSupervisorWatchCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := supervisorWatchAutonomousCmd()
	c.SetArgs(append([]string{"supervisor-watch"}, args...))
	out := &strings.Builder{}
	c.SetOut(out)
	err := c.Execute()
	return out.String(), err
}

func TestSupervisorWatchCLIRegisteredUnderAutonomous(t *testing.T) {
	found := false
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "supervisor-watch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("supervisor-watch is not registered under the autonomous command")
	}
}

func TestSupervisorWatchCLIText(t *testing.T) {
	getMethod, getPath := supervisorWatchDaemon(t, http.StatusOK, supervisorWatchBody)
	s, err := runSupervisorWatchCmd(t)
	if err != nil {
		t.Fatalf("execute: %v (output: %q)", err, s)
	}
	if m := getMethod(); m != http.MethodGet {
		t.Errorf("method = %q, want GET", m)
	}
	if p := getPath(); p != "/api/autonomous/supervisor-watch" {
		t.Errorf("path = %q, want the supervisor watch read route", p)
	}
	for _, want := range []string{
		"mode: shadow",
		"Last poll: 2026-10-29 10:15:00",
		"task_failed", "would alert", "a task in Automaton a1b2c3d4 failed",
		"ci_red", "alerted", "CI run 9001 is red on main",
		"Comparison: differs at 2026-10-29 10:15:00",
		"days of full agreement: 3",
		"only the watch raised: task_failed/a1b2c3d4",
		"only the script raised: ci_red/9001",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	// One line per finding.
	if n := strings.Count(s, "task_failed"); n != 2 { // the finding line + the comparison line
		t.Errorf("task_failed appears %d times, want 2:\n%s", n, s)
	}
	if strings.Contains(strings.ToLower(s), "prd") {
		t.Errorf("operator output must not say PRD:\n%s", s)
	}
}

func TestSupervisorWatchCLIJSONUntouched(t *testing.T) {
	supervisorWatchDaemon(t, http.StatusOK, supervisorWatchBody)
	s, err := runSupervisorWatchCmd(t, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if s != supervisorWatchBody {
		t.Errorf("--json did not pass the body through untouched:\n%s", s)
	}
}

func TestSupervisorWatchCLIEmptyFindings(t *testing.T) {
	supervisorWatchDaemon(t, http.StatusOK,
		`{"mode":"shadow","last_poll_at":"0001-01-01T00:00:00Z","findings":[],"priority_bands":[],"comparison":{"last":null,"agreed_days":0,"only_watch":[],"only_script":[]}}`)
	s, err := runSupervisorWatchCmd(t)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{"mode: shadow", "Last poll: never", "No supervisor watch findings.", "Comparison: none recorded yet"} {
		if !strings.Contains(s, want) {
			t.Errorf("empty state lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "priority band") {
		t.Errorf("an empty band list must not print band lines:\n%s", s)
	}
}

func TestSupervisorWatchCLIPriorityBands(t *testing.T) {
	supervisorWatchDaemon(t, http.StatusOK, `{
  "mode": "shadow",
  "last_poll_at": "2026-10-29T10:15:00Z",
  "findings": [],
  "priority_bands": [
    {"band": "high", "priority_from": 80, "priority_to": 100, "waiting": 2, "oldest_waiting_minutes": 41, "stalled": 1},
    {"band": "low", "priority_from": 0, "priority_to": 49, "waiting": 1, "oldest_waiting_minutes": 5, "stalled": 0}
  ],
  "comparison": {"last": null, "agreed_days": 0, "only_watch": [], "only_script": []}
}`)
	s, err := runSupervisorWatchCmd(t)
	if err != nil {
		t.Fatalf("execute: %v (output: %q)", err, s)
	}
	for _, want := range []string{
		"priority band high: 2 waiting (oldest 41 min, 1 stalled)",
		"priority band low: 1 waiting (oldest 5 min, 0 stalled)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("band output lacks %q:\n%s", want, s)
		}
	}
	if i := strings.Index(s, "priority band high"); i < 0 {
		t.Fatalf("high band line missing:\n%s", s)
	} else if j := strings.Index(s, "priority band low"); j < 0 || j < i {
		t.Errorf("bands must be in high, medium, low order:\n%s", s)
	}
	if strings.Contains(s, "priority band medium") {
		t.Errorf("a band that waits nothing must not get a line:\n%s", s)
	}
}

func TestSupervisorWatchCLIServerErrorExitCode(t *testing.T) {
	supervisorWatchDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	s, err := runSupervisorWatchCmd(t)
	if err == nil {
		t.Fatalf("a server error must fail the command (non-zero exit); output: %q", s)
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "autonomous disabled") {
		t.Errorf("error %q does not carry the status and the server message", err)
	}
	if s != "" {
		t.Errorf("nothing should be printed on a server error, got %q", s)
	}
}

const priorityStallsBody = `{"stalls":[{"automaton_id":"a1b2","priority":90,"waiting_minutes":45,"blocker_code":"held","blocker_text":"the Automaton is on hold","action":"release the hold"}]}`

func runPriorityStallsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := &cobra.Command{Use: "autonomous", SilenceUsage: true, SilenceErrors: true}
	c.AddCommand(newAutonomousPriorityStallsCmd())
	c.SetArgs(append([]string{"priority-stalls"}, args...))
	out := &strings.Builder{}
	c.SetOut(out)
	err := c.Execute()
	return out.String(), err
}

func TestPriorityStallsCLI(t *testing.T) {
	found := false
	for _, sub := range newAutonomousCmd().Commands() {
		found = found || sub.Name() == "priority-stalls"
	}
	if !found {
		t.Fatal("priority-stalls is not registered under the autonomous command")
	}
	method, path := supervisorWatchDaemon(t, http.StatusOK, priorityStallsBody)
	s, err := runPriorityStallsCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if method() != http.MethodGet || path() != "/api/autonomous/priority-stalls" {
		t.Errorf("requested %s %s", method(), path())
	}
	for _, want := range []string{"a1b2", "held", "the Automaton is on hold", "release the hold"} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	s, err = runPriorityStallsCmd(t, "--json")
	if err != nil || s != priorityStallsBody {
		t.Errorf("--json not untouched: %v %q", err, s)
	}
}

func TestPriorityStallsCLIEmpty(t *testing.T) {
	supervisorWatchDaemon(t, http.StatusOK, `{"stalls":[]}`)
	s, err := runPriorityStallsCmd(t)
	if err != nil || !strings.Contains(s, "No priority stalls") {
		t.Errorf("err=%v out=%q", err, s)
	}
}
