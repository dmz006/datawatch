// plan_coverage_filters_cli_test.go — fakes the daemon and checks that
// the prd-coverage read sends the right plan_coverage read request,
// renders the suppressed counts and the filter state, passes --json
// through untouched, keeps the no-report message when the record is
// null, and that the help text says Automaton (never PRD).
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// planCoverageFiltersDaemon starts a fake daemon returning the given
// JSON body and records the request (method + URI), pointing serverURL
// at it.
func planCoverageFiltersDaemon(t *testing.T, body string) (reqMethod func() string, reqURI func() string) {
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

func planCoverageFiltersCmd() *cobra.Command {
	c := &cobra.Command{Use: "autonomous"}
	c.AddCommand(newAutonomousPRDCoverageCmd())
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c
}

const planCoverageFiltersBody = `{
  "id": "prdx1",
  "status": "planned",
  "plan_coverage": {
    "mode": "block",
    "min_ratio": 1,
    "report": {"extracted": 3, "covered": 3, "unmatched": []},
    "findings": [],
    "suppressed": {"created_files": 2, "requirements": 1},
    "filters": {"suppress_created_files": true, "scoped_requirements": true, "requirement_sections": ["scope", "constraints"], "min_requirement_chars": 30}
  },
  "filters": {"suppress_created_files": true, "scoped_requirements": true, "requirement_sections": ["scope", "constraints"], "min_requirement_chars": 30},
  "suppressed": {"created_files": 2, "requirements": 1, "total": 3}
}`

func TestPlanCoverageFilters_CLIRequestIsThePlanCoverageRead(t *testing.T) {
	method, uri := planCoverageFiltersDaemon(t, planCoverageFiltersBody)
	c := planCoverageFiltersCmd()
	c.SetArgs([]string{"prd-coverage", "prdx1"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v (output: %q)", err, out.String())
	}
	if m := method(); m != http.MethodGet {
		t.Errorf("method = %q, want GET", m)
	}
	if want := "/api/autonomous/prds/prdx1/plan_coverage"; uri() != want {
		t.Errorf("uri = %q, want %q", uri(), want)
	}
}

func TestPlanCoverageFilters_CLISummaryShowsCountsAndFilters(t *testing.T) {
	planCoverageFiltersDaemon(t, planCoverageFiltersBody)
	c := planCoverageFiltersCmd()
	c.SetArgs([]string{"prd-coverage", "prdx1"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Plan coverage for prdx1 (planned)") {
		t.Errorf("missing the id + status line:\n%s", s)
	}
	for _, want := range []string{
		"Suppressed by the plan coverage filters: 2 created-file finding(s), 1 requirement finding(s)",
		"suppress_created_files=true",
		"scoped_requirements=true",
		"requirement_sections=scope,constraints",
		"min_requirement_chars=30",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	// The indented record must still be printed, after the summary.
	if !strings.Contains(s, "\"mode\": \"block\"") {
		t.Errorf("indented plan_coverage record missing:\n%s", s)
	}
	idx := strings.Index(s, "Suppressed by the plan coverage filters")
	rec := strings.Index(s, "\"mode\": \"block\"")
	if idx < 0 || rec < idx {
		t.Errorf("summary lines must precede the indented record:\n%s", s)
	}
}

func TestPlanCoverageFilters_CLIJSONPrintsRawBody(t *testing.T) {
	planCoverageFiltersDaemon(t, planCoverageFiltersBody)
	c := planCoverageFiltersCmd()
	c.SetArgs([]string{"prd-coverage", "prdx1", "--json"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	s := out.String()
	for _, want := range []string{
		`"id": "prdx1"`,
		`"suppressed": {"created_files": 2, "requirements": 1, "total": 3}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("--json did not pass the raw body through, lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Suppressed by the plan coverage filters:") {
		t.Errorf("--json must print the raw body only, not the summary:\n%s", s)
	}
}

func TestPlanCoverageFilters_CLINullRecordKeepsNoReportMessage(t *testing.T) {
	planCoverageFiltersDaemon(t, `{"id": "prdx2", "status": "drafting", "plan_coverage": null, "filters": {"suppress_created_files": true, "scoped_requirements": true, "requirement_sections": ["scope", "constraints"], "min_requirement_chars": 30}, "suppressed": {"created_files": 0, "requirements": 0, "total": 0}}`)
	c := planCoverageFiltersCmd()
	c.SetArgs([]string{"prd-coverage", "prdx2"})
	out := &strings.Builder{}
	c.SetOut(out)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "No plan-coverage report stored for prdx2") {
		t.Errorf("null record must keep the no-report message:\n%s", s)
	}
	if strings.Contains(s, "Suppressed by the plan coverage filters:") {
		t.Errorf("no-report state must not print the suppressed-count line:\n%s", s)
	}
}

func TestPlanCoverageFilters_CLIHelpSaysAutomatonNotPRD(t *testing.T) {
	cmd := newAutonomousPRDCoverageCmd()
	combined := cmd.Short + "\n" + cmd.Long
	if strings.Contains(combined, "PRD") {
		t.Errorf("help text must not say PRD:\n%s", combined)
	}
	if !strings.Contains(combined, "Automaton") {
		t.Errorf("help text must say Automaton:\n%s", combined)
	}
	if !strings.Contains(strings.ToLower(combined), "suppressed") {
		t.Errorf("help text must mention the suppressed counts:\n%s", combined)
	}
}
