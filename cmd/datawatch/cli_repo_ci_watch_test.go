// cli_repo_ci_watch_test.go — fakes the daemon and checks that the CLI
// sends the right read request, renders the watch state as the operator
// table (and the explicit no-records note) and passes --json through
// untouched.
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// repoCIWatchDaemon starts a fake daemon returning the given JSON body and
// records the request (method + full query), pointing serverURL at it.
func repoCIWatchDaemon(t *testing.T, body string) (reqMethod func() string, reqURI func() string) {
	t.Helper()
	var mu sync.Mutex
	method, uri := "", ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, uri = r.Method, r.URL.RequestURI()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return func() string { mu.Lock(); defer mu.Unlock(); return method },
		func() string { mu.Lock(); defer mu.Unlock(); return uri }
}

func repoCIWatchAutonomousCmd() *cobra.Command {
	c := &cobra.Command{Use: "autonomous"}
	c.AddCommand(newAutonomousRepoCIWatchCmd())
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c
}

const repoCIWatchBody = `{
  "mode": "on",
  "alert_minutes": 30,
  "repos": ["dmz006/datawatch"],
  "last_poll_at": "2026-10-29T10:15:00Z",
  "records": [
    {
      "repo": "dmz006/datawatch",
      "ref_kind": "main",
      "ref_name": "main",
      "sha": "abc123",
      "workflow": "ci.yml",
      "run_id": "9001",
      "status": "failing",
      "classification": "code_or_test",
      "first_seen_at": "2026-10-29T09:40:00Z",
      "updated_at": "2026-10-29T10:00:00Z",
      "alerted_at": "2026-10-29T10:10:00Z"
    },
    {
      "repo": "dmz006/datawatch",
      "ref_kind": "tag",
      "ref_name": "v1.1.0",
      "sha": "def456",
      "workflow": "ci.yml",
      "run_id": "9002",
      "status": "pending",
      "first_seen_at": "2026-10-29T10:14:30Z",
      "updated_at": "2026-10-29T10:14:30Z"
    }
  ]
}`

func TestRepoCIWatchCLIRegistered(t *testing.T) {
	c := repoCIWatchAutonomousCmd()
	found := false
	for _, sub := range c.Commands() {
		if sub.Name() == "repo-ci-watch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("repo-ci-watch is not registered under the autonomous command")
	}
}

func TestRepoCIWatchCLITable(t *testing.T) {
	repoCIWatchDaemon(t, repoCIWatchBody)
	c := repoCIWatchAutonomousCmd()
	c.SetArgs([]string{"repo-ci-watch"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %q)", err, out.String())
	}
	s := out.String()
	for _, col := range []string{"repository", "ref", "workflow", "status", "classification", "first seen", "alerted"} {
		if !strings.Contains(strings.ToLower(s), col) {
			t.Errorf("table lacks the %q column:\n%s", col, s)
		}
	}
	for _, want := range []string{"dmz006/datawatch", "main/main", "tag/v1.1.0", "ci.yml", "failing", "code_or_test", "pending"} {
		if !strings.Contains(s, want) {
			t.Errorf("table lacks %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "yes") {
		t.Errorf("alerted record not shown as yes:\n%s", s)
	}
	if !strings.Contains(s, "no") {
		t.Errorf("unalerted record not shown as no:\n%s", s)
	}
}

func TestRepoCIWatchCLISendsReadRequest(t *testing.T) {
	getMethod, getURI := repoCIWatchDaemon(t, repoCIWatchBody)
	c := repoCIWatchAutonomousCmd()
	c.SetArgs([]string{"repo-ci-watch", "--repo", "dmz006/datawatch", "--status", "failing"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if m := getMethod(); m != http.MethodGet {
		t.Errorf("method = %q, want GET", m)
	}
	u := getURI()
	if !strings.Contains(u, "/api/autonomous/repo-ci-watch") {
		t.Errorf("uri %q does not hit the repo CI watch read route", u)
	}
	if !strings.Contains(u, "repo=dmz006%2Fdatawatch") {
		t.Errorf("uri %q does not send the repo filter", u)
	}
	if !strings.Contains(u, "status=failing") {
		t.Errorf("uri %q does not send the status filter", u)
	}
}

func TestRepoCIWatchCLIJSONUntouched(t *testing.T) {
	repoCIWatchDaemon(t, repoCIWatchBody)
	c := repoCIWatchAutonomousCmd()
	c.SetArgs([]string{"repo-ci-watch", "--json"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, `"alert_minutes": 30`) {
		t.Errorf("--json did not pass the body through: %s", s)
	}
	if !strings.Contains(s, `"last_poll_at": "2026-10-29T10:15:00Z"`) {
		t.Errorf("--json lost last_poll_at: %s", s)
	}
}

func TestRepoCIWatchCLIEmptyState(t *testing.T) {
	repoCIWatchDaemon(t, `{"mode":"off","alert_minutes":0,"repos":[],"last_poll_at":"2026-10-29T00:00:00Z","records":[]}`)
	c := repoCIWatchAutonomousCmd()
	c.SetArgs([]string{"repo-ci-watch"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	s := out.String()
	if !strings.Contains(strings.ToLower(s), "no repo ci watch records") {
		t.Errorf("empty state does not say there are no records: %q", s)
	}
}
