package router

import (
	"strings"
	"testing"
)

func TestCostReportTextShowsColumnsNotesAttribution(t *testing.T) {
	body := `{"group_by":"automaton","rows":[{"key":"a1","sessions":2,"tokens_in":10,"tokens_out":5,"cache_read":7,"cache_creation":3,"tool_calls":4,"span_seconds":90,"active_seconds":60,"window_note":"partial","api_equivalent_usd":1.5}],"window_note":"since lifetime","attribution":{"automaton_launched":5,"unattributed":1,"ok":false}}`
	got := formatCostReportText(body)
	for _, want := range []string{"a1: 2 sessions", "10/5/7+3", "4 tools", "span 1m30s", "active 1m0s", "$1.50",
		"window note (a1): partial", "window note: since lifetime", "attribution: UNATTRIBUTED, 1 of 5"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestCostReportTextNotAReport(t *testing.T) {
	if formatCostReportText(`{"error":"x"}`) != "" || formatCostReportText("nope") != "" {
		t.Fatal("non-report body must fall through")
	}
}

func TestAutomatonStatusCostLines(t *testing.T) {
	got := automatonCostStatusLines(`{"cost":{"sessions":3,"tokens_in":1,"tokens_out":2,"usd_estimate":0.25,"window_note":"w"}}`)
	if len(got) != 2 || !strings.Contains(got[0], "3 sessions") || !strings.Contains(got[0], "$0.25") || got[1] != "cost window note: w" {
		t.Fatalf("got %v", got)
	}
	if automatonCostStatusLines(`{"id":"x"}`) != nil {
		t.Fatal("no cost must give nil")
	}
}
