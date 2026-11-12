// `cost charts` comm-channel verb tests: the verb reads the three REST views
// (cost over time, tool usage, retries and failures) through the loopback,
// forwards group_by and window to each, and replies with the joined text. It
// must not disturb the bare `cost` and `cost report` parsing.

package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const chartsCommSeries = `{"group_by":"automaton","bucket":"hour","window":"24h0m0s","buckets":["2026-11-01T00:00:00Z"],
"groups":[{"key":"Docs refresh","sessions":2,"tokens_in":900,"tokens_out":100,"tokens":1000,
"unit_totals":[{"unit":"points","billing_mode":"allowance","sessions":2,"tokens_in":900,"tokens_out":100,"active_seconds":30,"points":1.25,"percent":4}],"points":[]}],
"retention_days":31}`

const chartsCommTools = `{"group_by":"automaton","bucket":"hour","window":"24h0m0s","buckets":["2026-11-01T00:00:00Z"],
"groups":[{"key":"Docs refresh","sessions":2,"tool_calls":9,"points":[]}],"total_tool_calls":9,"total_sessions":2,
"tool_names_note":"no per-tool-name breakdown","retention_days":31}`

const chartsCommFailures = `{"group_by":"automaton","window":"24h0m0s","weeks":[],
"groups":[{"key":"a1","label":"Docs refresh","attempts":2,"tasks":1,"retries":2,"categories":[{"category":"stalled","attempts":2}],"weeks":[]}],
"categories":[{"category":"stalled","attempts":2}],"total_attempts":2,"total_tasks":1,"total_retries":2,"retry_note":"retries is a floor"}`

type chartsCommRec struct {
	mu    sync.Mutex
	paths map[string]url.Values
	fail  string
}

func (r *chartsCommRec) seen() map[string]url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]url.Values{}
	for k, v := range r.paths {
		out[k] = v
	}
	return out
}

func newChartsCommDaemon(t *testing.T, rec *chartsCommRec) *httptest.Server {
	t.Helper()
	rec.paths = map[string]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.mu.Lock()
		rec.paths[req.URL.Path] = req.URL.Query()
		rec.mu.Unlock()
		if req.URL.Path == rec.fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/cost/series":
			_, _ = io.WriteString(w, chartsCommSeries)
		case "/api/cost/tool-usage":
			_, _ = io.WriteString(w, chartsCommTools)
		case "/api/cost/failures":
			_, _ = io.WriteString(w, chartsCommFailures)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCostCharts_Parse(t *testing.T) {
	c := Parse("cost charts")
	if c.Type != CmdCostCharts || c.Text != "" {
		t.Errorf("bare = %+v", c)
	}
	c = Parse("cost charts group_by=project window=7d")
	if c.Type != CmdCostCharts || c.Text != "group_by=project window=7d" {
		t.Errorf("with args = %+v", c)
	}
	if c := Parse("Cost Charts"); c.Type != CmdCostCharts {
		t.Errorf("case-insensitive = %+v", c)
	}
	// neighbours keep their meaning
	if c := Parse("cost chartsx"); c.Type != CmdCost {
		t.Errorf("cost chartsx = %+v (must stay the cost command)", c)
	}
	if c := Parse("cost report"); c.Type != CmdCostReport {
		t.Errorf("cost report = %+v", c)
	}
	if c := Parse("cost"); c.Type != CmdCost {
		t.Errorf("cost = %+v", c)
	}
}

func TestCostCharts_Command_Dispatch(t *testing.T) {
	var rec chartsCommRec
	srv := newChartsCommDaemon(t, &rec)
	r := loopbackRouter(t, srv)

	out := strings.Join(r.HandleTestMessage("cost charts group_by=automaton window=24h"), "\n")

	seen := rec.seen()
	for _, path := range []string{"/api/cost/series", "/api/cost/tool-usage", "/api/cost/failures"} {
		q, ok := seen[path]
		if !ok {
			t.Fatalf("route %s was not read; reply:\n%s", path, out)
		}
		if q.Get("group_by") != "automaton" || q.Get("window") != "24h" {
			t.Errorf("%s query = %v", path, q)
		}
	}
	for _, want := range []string{
		"cost charts",
		"usage report: grouped by automaton, window 24h",
		"Docs refresh: 2 sessions, 1000 tokens",
		"points (allowance) points 1.25 (4.0% of the allowance)",
		"Docs refresh: 9 tool calls over 2 sessions",
		"Docs refresh: 2 failed attempts over 1 tasks, 2 retries",
		"categories: stalled 2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "failed:") || strings.Contains(strings.ToLower(out), "prd") {
		t.Errorf("reply reports failure or says PRD:\n%s", out)
	}
}

func TestCostCharts_Command_BareForwardsNoQuery(t *testing.T) {
	var rec chartsCommRec
	srv := newChartsCommDaemon(t, &rec)
	r := loopbackRouter(t, srv)
	_ = r.HandleTestMessage("cost charts")
	seen := rec.seen()
	if len(seen) != 3 {
		t.Fatalf("expected 3 routes read, saw %v", seen)
	}
	for path, q := range seen {
		if len(q) != 0 {
			t.Errorf("%s: bare verb must forward no query, got %v", path, q)
		}
	}
}

func TestCostCharts_Command_GroupByAutomationSplits(t *testing.T) {
	var rec chartsCommRec
	srv := newChartsCommDaemon(t, &rec)
	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("cost charts group_by=automation"), "\n")
	seen := rec.seen()
	if got := seen["/api/cost/failures"].Get("group_by"); got != "automation" {
		t.Errorf("failures group_by = %q want automation", got)
	}
	if got := seen["/api/cost/series"].Get("group_by"); got != "" {
		t.Errorf("series group_by = %q want none", got)
	}
	if !strings.Contains(out, "are grouped by automaton (the default), not automation") {
		t.Errorf("reply must say the series views kept their default:\n%s", out)
	}
}

func TestCostCharts_Command_BadInput(t *testing.T) {
	r := &Router{hostname: "h"}
	for _, in := range []string{"cost charts bogus=1", "cost charts notakey"} {
		out := strings.Join(r.HandleTestMessage(in), "\n")
		if !strings.Contains(out, "usage: cost charts") {
			t.Errorf("%q: expected usage hint, got:\n%s", in, out)
		}
	}
	// a bad grouping and a failing view both come back as a failure reply
	var rec chartsCommRec
	srv := newChartsCommDaemon(t, &rec)
	lr := loopbackRouter(t, srv)
	if out := strings.Join(lr.HandleTestMessage("cost charts group_by=bogus"), "\n"); !strings.Contains(out, "cost charts failed") || !strings.Contains(out, "group_by must be one of") {
		t.Errorf("bad group_by reply:\n%s", out)
	}
	rec.fail = "/api/cost/failures"
	if out := strings.Join(lr.HandleTestMessage("cost charts"), "\n"); !strings.Contains(out, "cost charts failed") || !strings.Contains(out, "retries and failures") {
		t.Errorf("failing view reply:\n%s", out)
	}
}

func TestCostCharts_Command_NoLoopback(t *testing.T) {
	r := &Router{hostname: "h"}
	out := strings.Join(r.HandleTestMessage("cost charts"), "\n")
	if !strings.Contains(out, "cost charts failed") {
		t.Errorf("expected a failure reply without a loopback:\n%s", out)
	}
}

func TestCostCharts_HelpListsVerb(t *testing.T) {
	r := &Router{hostname: "h"}
	out := strings.Join(r.HandleTestMessage("help"), "\n")
	if !strings.Contains(out, "cost charts [group_by=") {
		t.Errorf("help misses the verb:\n%s", out[:min(2000, len(out))])
	}
}
