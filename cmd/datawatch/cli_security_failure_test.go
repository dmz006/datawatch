// cli_security_failure_test.go — fakes the daemon and checks that the
// security-failure command reads through the REST route, prints the
// operator table (or the body untouched with --json), and never sends a
// write request.
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

const securityFailureCLIBody = `[
  {"repo":"dmz006/datawatch","ref_kind":"main","ref_name":"main","workflow":"security.yml",
   "run_id":"7001","run_url":"https://github.com/dmz006/datawatch/actions/runs/7001","status":"failing",
   "report":{"dependency":"golang.org/x/net","advisory_ids":["GHSA-aaaa-bbbb-cccc"],"workflow":"security.yml",
     "run_id":7001,"published_at":"2026-10-01","recommendation":"upgrade","mode":"shadow","action":"would_stop"}},
  {"repo":"dmz006/datawatch","ref_kind":"tag","ref_name":"v1.2.0","workflow":"security.yml",
   "run_id":"7002","status":"green",
   "report":{"dependency":"example.com/old","advisory_ids":["CVE-2026-0001"],"run_id":7002,
     "recommendation":"keep","mode":"on","action":"stopped"}}
]`

// securityFailureDaemon starts a fake daemon returning body and counts the
// read and write requests it sees, pointing serverURL at it.
func securityFailureDaemon(t *testing.T, body string) (reads func() int, writes func() int, uri func() string) {
	t.Helper()
	var mu sync.Mutex
	nReads, nWrites, last := 0, 0, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.Method == http.MethodGet {
			nReads++
		} else {
			nWrites++
		}
		last = r.URL.RequestURI()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	get := func(p *int) func() int { return func() int { mu.Lock(); defer mu.Unlock(); return *p } }
	return get(&nReads), get(&nWrites), func() string { mu.Lock(); defer mu.Unlock(); return last }
}

func securityFailureAutonomousCmd() *cobra.Command {
	c := &cobra.Command{Use: "autonomous"}
	c.AddCommand(newAutonomousSecurityFailureCmd())
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c
}

func runSecurityFailureCLI(t *testing.T, args ...string) string {
	t.Helper()
	c := securityFailureAutonomousCmd()
	c.SetArgs(append([]string{"security-failure"}, args...))
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %q)", err, out.String())
	}
	return out.String()
}

func TestSecurityFailureCLITable(t *testing.T) {
	reads, writes, _ := securityFailureDaemon(t, securityFailureCLIBody)
	s := runSecurityFailureCLI(t)
	for _, col := range []string{"repository", "ref", "workflow", "run", "status", "dependency", "advisory", "recommendation", "action"} {
		if !strings.Contains(strings.ToLower(s), col) {
			t.Errorf("table lacks the %q column:\n%s", col, s)
		}
	}
	for _, want := range []string{"dmz006/datawatch", "main", "security.yml", "7001", "failing", "golang.org/x/net", "GHSA-aaaa-bbbb-cccc", "upgrade", "would_stop"} {
		if !strings.Contains(s, want) {
			t.Errorf("table lacks %q:\n%s", want, s)
		}
	}
	// The green run is closed: without --status it is not an open report.
	if strings.Contains(s, "7002") {
		t.Errorf("a green run is listed as open:\n%s", s)
	}
	if strings.Contains(s, "{") || strings.Contains(s, "\"run_id\"") {
		t.Errorf("table output looks like JSON:\n%s", s)
	}
	if reads() != 1 || writes() != 0 {
		t.Errorf("reads=%d writes=%d, want 1 read and 0 writes", reads(), writes())
	}
}

func TestSecurityFailureCLIJSON(t *testing.T) {
	_, writes, _ := securityFailureDaemon(t, securityFailureCLIBody)
	s := runSecurityFailureCLI(t, "--json")
	if s != securityFailureCLIBody {
		t.Errorf("--json did not pass the body through untouched:\n%s", s)
	}
	if strings.Contains(s, "REPOSITORY") {
		t.Errorf("--json printed the table:\n%s", s)
	}
	if writes() != 0 {
		t.Errorf("writes=%d, want 0", writes())
	}
}

func TestSecurityFailureCLIFakeServerFilters(t *testing.T) {
	_, writes, uri := securityFailureDaemon(t, securityFailureCLIBody)
	s := runSecurityFailureCLI(t, "--repo", "dmz006/datawatch", "--status", "green")
	u := uri()
	for _, want := range []string{"/api/autonomous/security-failures", "repo=dmz006%2Fdatawatch", "status=green"} {
		if !strings.Contains(u, want) {
			t.Errorf("uri %q lacks %q", u, want)
		}
	}
	// An explicit --status shows what the daemon returned, closed runs included.
	if !strings.Contains(s, "7002") {
		t.Errorf("explicit --status green hid the green run:\n%s", s)
	}
	if writes() != 0 {
		t.Errorf("writes=%d, want 0", writes())
	}
}

func TestSecurityFailureCLIEmpty(t *testing.T) {
	securityFailureDaemon(t, `[]`)
	s := runSecurityFailureCLI(t)
	if !strings.Contains(strings.ToLower(s), "no open security failure reports") {
		t.Errorf("empty list does not say so: %q", s)
	}
}

func TestSecurityFailureCLIRegistered(t *testing.T) {
	found := false
	for _, sub := range newAutonomousCmd().Commands() {
		if sub.Name() == "security-failure" {
			found = true
		}
	}
	if !found {
		t.Fatal("security-failure is not registered under the autonomous command")
	}
}
