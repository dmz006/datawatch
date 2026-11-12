package costreport

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

const chartsSeriesBody = `{"group_by":"automaton","bucket":"day","window":"168h0m0s","since":"2026-11-01T00:00:00Z","until":"2026-11-08T00:00:00Z",
"buckets":["2026-11-01T00:00:00Z","2026-11-02T00:00:00Z"],
"groups":[{"key":"Nightly docs","sessions":3,"tokens_in":1000,"tokens_out":200,"tokens":1200,
"unit_totals":[{"unit":"dollars","billing_mode":"api","sessions":3,"tokens_in":1000,"tokens_out":200,"active_seconds":60,"dollars":1.5}],"points":[]}],
"totals_by_unit":[{"unit":"points","billing_mode":"allowance","sessions":3,"tokens_in":1000,"tokens_out":200,"active_seconds":60,"points":2.5,"percent":12.5}],
"retention_days":31,"unknown_spend_note":"spend before 2026-10-01 is not recorded"}`

const chartsToolBody = `{"group_by":"automaton","bucket":"day","window":"168h0m0s","buckets":["2026-11-01T00:00:00Z"],
"groups":[{"key":"Nightly docs","sessions":3,"tool_calls":42,"points":[]}],
"total_tool_calls":42,"total_sessions":3,"tool_names_note":"the ledger keeps a count, never a tool name","retention_days":31}`

const chartsFailuresBody = `{"group_by":"project","window":"2016h0m0s","weeks":["2026-11-02T00:00:00Z"],
"groups":[{"key":"datawatch","label":"datawatch","attempts":4,"tasks":2,"retries":3,
"categories":[{"category":"timeout","attempts":3},{"category":"uncategorized","attempts":1}],"weeks":[]}],
"categories":[{"category":"timeout","attempts":3},{"category":"uncategorized","attempts":1}],
"total_attempts":4,"total_tasks":2,"total_retries":3,"undated_attempts":1,"retry_note":"retries is a floor"}`

func TestChartsQueries_GroupByRouting(t *testing.T) {
	cases := []struct {
		gb               string
		seriesGB, failGB string
		wantNotes        int
		wantErr          bool
	}{
		{"", "", "", 0, false},
		{"automaton", "automaton", "automaton", 0, false},
		{"project", "project", "project", 0, false},
		{"MODEL", "model", "", 1, false},
		{"automation", "", "automation", 1, false},
		{"bogus", "", "", 0, true},
	}
	for _, c := range cases {
		sq, tq, fq, notes, err := ChartsQueries(ChartsRequest{GroupBy: c.gb, Window: " 7d "})
		if (err != nil) != c.wantErr {
			t.Fatalf("%q: err = %v", c.gb, err)
		}
		if c.wantErr {
			continue
		}
		if sq.Get("group_by") != c.seriesGB || tq.Get("group_by") != c.seriesGB {
			t.Errorf("%q: series/tool group_by = %q/%q want %q", c.gb, sq.Get("group_by"), tq.Get("group_by"), c.seriesGB)
		}
		if fq.Get("group_by") != c.failGB {
			t.Errorf("%q: failures group_by = %q want %q", c.gb, fq.Get("group_by"), c.failGB)
		}
		if len(notes) != c.wantNotes {
			t.Errorf("%q: notes = %v want %d", c.gb, notes, c.wantNotes)
		}
		for _, q := range []url.Values{sq, tq, fq} {
			if q.Get("window") != "7d" {
				t.Errorf("%q: window = %q want 7d", c.gb, q.Get("window"))
			}
		}
	}
}

func TestFetchCharts_JoinsThreeViewsAndRenders(t *testing.T) {
	seen := map[string]url.Values{}
	get := func(path string, q url.Values) ([]byte, error) {
		seen[path] = q
		switch path {
		case ChartsSeriesPath:
			return []byte(chartsSeriesBody), nil
		case ChartsToolUsagePath:
			return []byte(chartsToolBody), nil
		case ChartsFailuresPath:
			return []byte(chartsFailuresBody), nil
		}
		return nil, errors.New("unexpected path " + path)
	}
	c, err := FetchCharts(ChartsRequest{GroupBy: "automation", Window: "7d"}, get)
	if err != nil {
		t.Fatalf("FetchCharts: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 routes read, saw %v", seen)
	}
	if c.CostOverTime == nil || c.ToolUsage == nil || c.Failures == nil {
		t.Fatalf("a view is missing: %+v", c)
	}
	text := c.Text()
	for _, want := range []string{
		"usage report: grouped by automation, window 7d",
		"cost over time (by automaton, window 168h0m0s, 2 day buckets)",
		"Nightly docs: 3 sessions, 1200 tokens (in 1000, out 200)",
		"dollars (api) dollars $1.50",
		"total: points (allowance) points 2.50 (12.5% of the allowance)",
		"note: spend before 2026-10-01 is not recorded",
		"Nightly docs: 42 tool calls over 3 sessions",
		"total: 42 tool calls over 3 sessions",
		"never a tool name",
		"datawatch: 4 failed attempts over 2 tasks, 3 retries",
		"categories: timeout 3, uncategorized 1",
		"failure categories: timeout 3, uncategorized 1",
		"1 attempts carry no time",
		"note: retries is a floor",
		"cost over time and tool usage are grouped by automaton (the default), not automation",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "prd") {
		t.Errorf("operator text must not say PRD:\n%s", text)
	}
}

func TestFetchCharts_ViewFailureNamesView(t *testing.T) {
	get := func(path string, _ url.Values) ([]byte, error) {
		if path == ChartsFailuresPath {
			return nil, errors.New("HTTP 503: autonomous manager not available")
		}
		if path == ChartsSeriesPath {
			return []byte(chartsSeriesBody), nil
		}
		return []byte(chartsToolBody), nil
	}
	_, err := FetchCharts(ChartsRequest{}, get)
	if err == nil || !strings.Contains(err.Error(), "retries and failures") || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v", err)
	}
	if _, err := FetchCharts(ChartsRequest{GroupBy: "nope"}, get); err == nil {
		t.Fatal("bad group_by must fail before any read")
	}
}

func TestJoinCharts_BadBody(t *testing.T) {
	if _, err := JoinCharts([]byte("nope"), []byte(chartsToolBody), []byte(chartsFailuresBody)); err == nil || !strings.Contains(err.Error(), "cost over time") {
		t.Fatalf("err = %v", err)
	}
}

func TestChartsJSONRoundTrip(t *testing.T) {
	c, err := JoinCharts([]byte(chartsSeriesBody), []byte(chartsToolBody), []byte(chartsFailuresBody))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(out, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cost_over_time", "tool_usage", "failures"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("payload misses section %q", k)
		}
	}
	back, err := ParseCharts(out)
	if err != nil {
		t.Fatalf("ParseCharts: %v", err)
	}
	if back.Text() != c.Text() {
		t.Errorf("text changed over the round trip")
	}
	if _, err := ParseCharts([]byte(`{"cost_over_time":{}}`)); err == nil {
		t.Error("a payload with a missing view must not parse")
	}
}

func TestChartsText_EmptyViews(t *testing.T) {
	text := (&Charts{CostOverTime: &Series{}, ToolUsage: &ToolUsage{}, Failures: &Failures{}}).Text()
	for _, want := range []string{"no spend in the window", "no tool calls in the window", "no failed, stalled or cancelled attempts in the window"} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
	if (*Charts)(nil).Text() != "" {
		t.Error("nil charts must render empty")
	}
}
