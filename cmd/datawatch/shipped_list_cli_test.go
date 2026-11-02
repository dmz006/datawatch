// Tests for the shipped-state CLI query builders (shipped.go): prdListQuery
// and automationListPath are the daemon-independent halves of `autonomous
// prd-list --status/--shipped` and `automation list --shipped` — they build
// the exact query the daemon receives. No daemon, no home directory.

package main

import (
	"strings"
	"testing"
)

// TestShippedCLI_PrdListQuery: --status / --shipped compose the PRD list
// query. Neither flag = the full list (full=1); shipped=true|false narrows
// to the shipped / unshipped side; status may carry the shipped bucket; the
// two AND. A shipped value that is neither true nor false is refused before
// any daemon call.
func TestShippedCLI_PrdListQuery(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		shipped string
		want    string
	}{
		{"neither set is the full list", "", "", "full=1"},
		{"shipped true narrows to shipped", "", "true", "shipped=true"},
		{"shipped false narrows to unshipped", "", "false", "shipped=false"},
		{"shipped all is the daemon default view (no query)", "", "all", ""},
		{"status may carry the shipped bucket", "shipped", "", "status=shipped"},
		{"statuses plus shipped AND", "completed,cancelled", "false", "shipped=false&status=completed%2Ccancelled"},
		{"shiptype status with both flags", "running,shipped", "true", "shipped=true&status=running%2Cshipped"},
		{"shipped is folded to case; status passes through (server normalizes)", "RUNNING", " True ", "shipped=true&status=RUNNING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := prdListQuery(tc.status, tc.shipped)
			if err != nil {
				t.Fatalf("prdListQuery(%q, %q): %v", tc.status, tc.shipped, err)
			}
			if got := q.Encode(); got != tc.want {
				t.Errorf("query = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestShippedCLI_PrdListQueryBogusShipped: shipped=banana must be refused
// with the operator-facing explanation, not sent to the daemon.
func TestShippedCLI_PrdListQueryBogusShipped(t *testing.T) {
	for _, v := range []string{"banana", "1", "yes"} {
		if _, err := prdListQuery("", v); err == nil {
			t.Errorf("shipped=%q: expected an error, got a query", v)
		} else if !strings.Contains(err.Error(), "shipped: must be true or false") {
			t.Errorf("shipped=%q error = %q; want the 'shipped: must be true or false' explanation", v, err)
		}
	}
}

// TestShippedCLI_AutomationListPath: --shipped composes the automation
// list path. Empty / all = the daemon's default view (no query); true /
// false = shipped state only / everything else. Bogus values are refused.
func TestShippedCLI_AutomationListPath(t *testing.T) {
	cases := []struct {
		name    string
		shipped string
		want    string
	}{
		{"empty is the daemon default view", "", "/api/automations"},
		{"all is the daemon default view", "all", "/api/automations"},
		{"true keeps shipped automations", "true", "/api/automations?shipped=true"},
		{"false keeps unshipped automations", "false", "/api/automations?shipped=false"},
		{"case is folded", " TRUE ", "/api/automations?shipped=true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := automationListPath(tc.shipped)
			if err != nil {
				t.Fatalf("automationListPath(%q): %v", tc.shipped, err)
			}
			if got != tc.want {
				t.Errorf("path = %q; want %q", got, tc.want)
			}
		})
	}

	if _, err := automationListPath("banana"); err == nil {
		t.Errorf("shipped=banana: expected an error, got a path")
	} else if !strings.Contains(err.Error(), "--shipped must be true or false") {
		t.Errorf("error = %q; want the '--shipped must be true or false' explanation", err)
	}
}
