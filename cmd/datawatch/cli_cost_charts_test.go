// `datawatch cost charts` tests. The CLI is a thin REST proxy over three
// routes, so these tests point it at a fake daemon (the --url override), pin
// the query forwarding to each route, the combined JSON payload and the text
// rendering.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const chartsFakeSeries = `{"group_by":"project","bucket":"day","window":"168h0m0s","buckets":["2026-11-01T00:00:00Z"],
"groups":[{"key":"datawatch","sessions":2,"tokens_in":900,"tokens_out":100,"tokens":1000,
"unit_totals":[{"unit":"dollars","billing_mode":"api","sessions":2,"tokens_in":900,"tokens_out":100,"active_seconds":30,"dollars":0.75}],"points":[]}],
"retention_days":31}`

const chartsFakeTools = `{"group_by":"project","bucket":"day","window":"168h0m0s","buckets":["2026-11-01T00:00:00Z"],
"groups":[{"key":"datawatch","sessions":2,"tool_calls":17,"points":[]}],"total_tool_calls":17,"total_sessions":2,
"tool_names_note":"no per-tool-name breakdown","retention_days":31}`

const chartsFakeFailures = `{"group_by":"project","window":"168h0m0s","weeks":[],
"groups":[{"key":"datawatch","attempts":3,"tasks":2,"retries":4,"categories":[{"category":"timeout","attempts":3}],"weeks":[]}],
"categories":[{"category":"timeout","attempts":3}],"total_attempts":3,"total_tasks":2,"total_retries":4,"retry_note":"retries is a floor"}`

type chartsDaemon struct {
	mu    sync.Mutex
	paths map[string]url.Values
	fail  string // path that answers 500
}

func (d *chartsDaemon) seen() map[string]url.Values {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]url.Values{}
	for k, v := range d.paths {
		out[k] = v
	}
	return out
}

func newChartsFakeDaemon(t *testing.T, d *chartsDaemon) *httptest.Server {
	t.Helper()
	d.paths = map[string]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		d.mu.Lock()
		d.paths[req.URL.Path] = req.URL.Query()
		d.mu.Unlock()
		if req.URL.Path == d.fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/cost/series":
			_, _ = io.WriteString(w, chartsFakeSeries)
		case "/api/cost/tool-usage":
			_, _ = io.WriteString(w, chartsFakeTools)
		case "/api/cost/failures":
			_, _ = io.WriteString(w, chartsFakeFailures)
		default:
			t.Errorf("unexpected path %q", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runCostCharts(t *testing.T, srv *httptest.Server, flags map[string]string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := withServerURL(t, srv, func() error {
		c := newCostChartsCmd()
		c.SetOut(&out)
		for k, v := range flags {
			if err := c.Flags().Set(k, v); err != nil {
				t.Fatalf("set --%s: %v", k, err)
			}
		}
		return c.RunE(c, nil)
	})
	return out.String(), err
}

func TestCostCharts_CLI_Flags(t *testing.T) {
	c := newCostChartsCmd()
	if c.Name() != "charts" {
		t.Errorf("name = %q want charts", c.Name())
	}
	for _, f := range []string{"group-by", "window", "json"} {
		if c.Flag(f) == nil {
			t.Errorf("--%s flag missing", f)
		}
	}
}

func TestCostCharts_CostParentIncludesCharts(t *testing.T) {
	found := false
	for _, sub := range newCostCmd().Commands() {
		if sub.Name() == "charts" {
			found = true
		}
	}
	if !found {
		t.Fatal("cost is missing the charts subcommand")
	}
}

func TestCostCharts_CLI_TextReadsThreeRoutes(t *testing.T) {
	var d chartsDaemon
	srv := newChartsFakeDaemon(t, &d)

	out, err := runCostCharts(t, srv, map[string]string{"group-by": "project", "window": "7d"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	seen := d.seen()
	for _, path := range []string{"/api/cost/series", "/api/cost/tool-usage", "/api/cost/failures"} {
		q, ok := seen[path]
		if !ok {
			t.Fatalf("route %s was not read", path)
		}
		if q.Get("group_by") != "project" || q.Get("window") != "7d" {
			t.Errorf("%s query = %v want group_by=project window=7d", path, q)
		}
	}
	for _, want := range []string{
		"usage report: grouped by project, window 7d",
		"cost over time", "datawatch: 2 sessions, 1000 tokens",
		"dollars (api) dollars $0.75",
		"tool usage", "datawatch: 17 tool calls over 2 sessions",
		"retries and failures", "datawatch: 3 failed attempts over 2 tasks, 4 retries",
		"categories: timeout 3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "prd") {
		t.Errorf("operator text must not say PRD:\n%s", out)
	}
}

func TestCostCharts_CLI_BareForwardsNoQuery(t *testing.T) {
	var d chartsDaemon
	srv := newChartsFakeDaemon(t, &d)
	if _, err := runCostCharts(t, srv, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	for path, q := range d.seen() {
		if len(q) != 0 {
			t.Errorf("%s: bare command must forward no query, got %v", path, q)
		}
	}
}

func TestCostCharts_CLI_JSONIsOnePayload(t *testing.T) {
	var d chartsDaemon
	srv := newChartsFakeDaemon(t, &d)
	out, err := runCostCharts(t, srv, map[string]string{"json": "true", "group-by": "model"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, out)
	}
	for _, k := range []string{"cost_over_time", "tool_usage", "failures", "notes"} {
		if _, ok := payload[k]; !ok {
			t.Errorf("payload misses %q", k)
		}
	}
	// model is not a failures grouping: that route gets no group_by.
	if got := d.seen()["/api/cost/failures"].Get("group_by"); got != "" {
		t.Errorf("failures group_by = %q want none", got)
	}
	if got := d.seen()["/api/cost/series"].Get("group_by"); got != "model" {
		t.Errorf("series group_by = %q want model", got)
	}
}

func TestCostCharts_CLI_Errors(t *testing.T) {
	var d chartsDaemon
	srv := newChartsFakeDaemon(t, &d)
	if _, err := runCostCharts(t, srv, map[string]string{"group-by": "bogus"}); err == nil || !strings.Contains(err.Error(), "group_by must be one of") {
		t.Errorf("bad group-by err = %v", err)
	}
	d.fail = "/api/cost/failures"
	if _, err := runCostCharts(t, srv, nil); err == nil || !strings.Contains(err.Error(), "retries and failures") {
		t.Errorf("failing view err = %v", err)
	}
}
