package router

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

// securityFailureCommSample is the security failure list body for the comm
// tests: an open shadow-mode report with a registry entry, an open report
// with an unknown publication date, and a closed (green) report.
const securityFailureCommSample = `[` +
	`{"repo":"dmz006/datawatch","ref_kind":"main","ref_name":"main","workflow":"security.yml",` +
	`"run_id":"7001","run_url":"https://github.com/dmz006/datawatch/actions/runs/7001","status":"failing",` +
	`"report":{"dependency":"golang.org/x/net","advisory_ids":["GHSA-aaaa-bbbb-cccc"],"workflow":"security.yml",` +
	`"run_id":7001,"published_at":"2026-10-01","registry_found":true,"registry_id":"R-12",` +
	`"registry_first_added":"2026-09-01","registry_added":"2026-09-20","registry_expires":"2027-03-20",` +
	`"recommendation":"keep","mode":"shadow","action":"would_stop"}},` +
	`{"repo":"dmz006/datawatch","ref_kind":"tag","ref_name":"v1.2.0","workflow":"security.yml",` +
	`"run_id":"7002","run_url":"https://github.com/dmz006/datawatch/actions/runs/7002","status":"hung",` +
	`"report":{"dependency":"example.com/old","advisory_ids":["CVE-2026-0001"],"run_id":7002,` +
	`"recommendation":"keep_no_entry","mode":"on","action":"stopped"}},` +
	`{"repo":"dmz006/datawatch","ref_kind":"main","ref_name":"main","workflow":"security.yml",` +
	`"run_id":"7003","status":"green",` +
	`"report":{"dependency":"example.com/closed","advisory_ids":["CVE-2026-0002"],"run_id":7003,` +
	`"recommendation":"upgrade","mode":"on","action":"report_only"}}]`

// securityFailureCommDaemon is a fake daemon that serves body and counts
// the read and write requests it receives. Every write path (anything but
// GET) lands on the write counter, which the tests assert is zero.
type securityFailureCommCalls struct {
	mu     sync.Mutex
	reads  int
	writes int
	path   string
}

func securityFailureCommDaemon(t *testing.T, body string) (*securityFailureCommCalls, *Router) {
	t.Helper()
	calls := &securityFailureCommCalls{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls.mu.Lock()
		if r.Method == http.MethodGet {
			calls.reads++
		} else {
			calls.writes++
		}
		calls.path = r.URL.Path
		calls.mu.Unlock()
		_, _ = w.Write([]byte(body))
	})
	return calls, setupPeerRouter(t, port)
}

func (c *securityFailureCommCalls) counts() (reads, writes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads, c.writes
}

func TestSecurityFailureCommVerbDispatch(t *testing.T) {
	if !slices.Contains(SecurityFailureCommVerbs(), "security-failure") {
		t.Fatalf("SecurityFailureCommVerbs() = %v, must contain security-failure", SecurityFailureCommVerbs())
	}
	for _, verb := range SecurityFailureCommVerbs() {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}
}

func TestSecurityFailureCommListsOpenReports(t *testing.T) {
	calls, r := securityFailureCommDaemon(t, securityFailureCommSample)
	got := strings.Join(r.HandleTestMessage("autonomous security-failure"), "\n")
	for _, want := range []string{
		"autonomous security-failure", "golang.org/x/net", "GHSA-aaaa-bbbb-cccc", "7001", "failing",
		"example.com/old", "CVE-2026-0001", "7002", "hung",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, "7003") || strings.Contains(got, "example.com/closed") {
		t.Errorf("the closed (green) report is listed as open: %q", got)
	}
	reads, writes := calls.counts()
	if reads != 1 || writes != 0 {
		t.Errorf("reads=%d writes=%d, want 1 read and 0 writes", reads, writes)
	}
	if calls.path != "/api/autonomous/security-failures" {
		t.Errorf("path = %s, want the security failure read route", calls.path)
	}

	// The underscore alias answers the same read.
	_, r = securityFailureCommDaemon(t, securityFailureCommSample)
	if got := strings.Join(r.HandleTestMessage("autonomous security_failure"), "\n"); !strings.Contains(got, "golang.org/x/net") {
		t.Errorf("the underscore alias must answer the read, got: %q", got)
	}
}

func TestSecurityFailureCommEmptyList(t *testing.T) {
	_, r := securityFailureCommDaemon(t, `[]`)
	got := strings.Join(r.HandleTestMessage("autonomous security-failure"), "\n")
	if !strings.Contains(got, "No open security failure reports.") {
		t.Errorf("empty list does not say so: %q", got)
	}
}

func TestSecurityFailureCommFullReportForRun(t *testing.T) {
	calls, r := securityFailureCommDaemon(t, securityFailureCommSample)
	got := strings.Join(r.HandleTestMessage("autonomous security-failure 7001"), "\n")
	for _, want := range []string{
		"Dependency: golang.org/x/net",
		"Advisory: GHSA-aaaa-bbbb-cccc",
		"Published: 2026-10-01",
		"Registry entry: R-12 (first added 2026-09-01, added 2026-09-20, expires 2027-03-20)",
		"Recommendation: keep",
		"Would have taken (shadow mode): stop the Automaton line",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("full report lacks %q: %q", want, got)
		}
	}
	if _, writes := calls.counts(); writes != 0 {
		t.Errorf("writes=%d, want 0", writes)
	}

	// Unknown publication date, no registry entry, and an action taken.
	_, r = securityFailureCommDaemon(t, securityFailureCommSample)
	got = strings.Join(r.HandleTestMessage("autonomous security-failure 7002"), "\n")
	for _, want := range []string{
		"Published: unknown",
		"Registry entry: none",
		"Recommendation: keep_no_entry",
		"Action taken: stopped the Automaton line",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("full report lacks %q: %q", want, got)
		}
	}

	// A closed report is still reachable by its run.
	_, r = securityFailureCommDaemon(t, securityFailureCommSample)
	if got := strings.Join(r.HandleTestMessage("autonomous security-failure 7003"), "\n"); !strings.Contains(got, "example.com/closed") {
		t.Errorf("closed report not reachable by run: %q", got)
	}
}

func TestSecurityFailureCommUnknownRunNotFound(t *testing.T) {
	calls, r := securityFailureCommDaemon(t, securityFailureCommSample)
	got := strings.Join(r.HandleTestMessage("autonomous security-failure 9999"), "\n")
	if !strings.Contains(got, "No security failure report found for run 9999.") {
		t.Errorf("unknown run did not get the not-found reply: %q", got)
	}
	if strings.Contains(got, "failed") {
		t.Errorf("not-found must not read as a failure: %q", got)
	}
	if _, writes := calls.counts(); writes != 0 {
		t.Errorf("writes=%d, want 0", writes)
	}
}

func TestSecurityFailureCommNoWritesNoPRDWording(t *testing.T) {
	calls, r := securityFailureCommDaemon(t, securityFailureCommSample)
	var all []string
	for _, msg := range []string{
		"autonomous security-failure", "autonomous security-failure 7001",
		"autonomous security-failure 7002", "autonomous security-failure 9999",
		"autonomous security-failure a b",
	} {
		all = append(all, r.HandleTestMessage(msg)...)
	}
	joined := strings.Join(all, "\n")
	if strings.Contains(joined, "PRD") || strings.Contains(joined, "prd") {
		t.Errorf("operator-facing text must say Automaton, never PRD: %q", joined)
	}
	if !strings.Contains(joined, "Automaton") {
		t.Errorf("the action line must say Automaton: %q", joined)
	}
	if !strings.Contains(joined, securityFailureUsage) {
		t.Errorf("extra arguments did not get the usage line: %q", joined)
	}
	if _, writes := calls.counts(); writes != 0 {
		t.Errorf("write path called %d times, want 0", writes)
	}
}
