// cost_charts — MCP tool tests: the combined usage report is read-only,
// registered, and returns one payload built from the three REST views.

package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/federation"
)

const chartsMCPSeries = `{"group_by":"model","bucket":"day","window":"168h0m0s","buckets":["2026-11-01T00:00:00Z"],
"groups":[{"key":"claude-code","sessions":5,"tokens_in":4000,"tokens_out":500,"tokens":4500,"points":[]}],"retention_days":31}`

const chartsMCPTools = `{"group_by":"model","bucket":"day","window":"168h0m0s","buckets":["2026-11-01T00:00:00Z"],
"groups":[{"key":"claude-code","sessions":5,"tool_calls":31,"points":[]}],"total_tool_calls":31,"total_sessions":5,
"tool_names_note":"no per-tool-name breakdown","retention_days":31}`

const chartsMCPFailures = `{"group_by":"project","window":"2016h0m0s","weeks":[],
"groups":[{"key":"datawatch","attempts":6,"tasks":3,"retries":5,"categories":[{"category":"timeout","attempts":6}],"weeks":[]}],
"categories":[{"category":"timeout","attempts":6}],"total_attempts":6,"total_tasks":3,"total_retries":5,"retry_note":"retries is a floor"}`

type chartsMCPRec struct {
	mu    sync.Mutex
	paths map[string]url.Values
	fail  string
}

func (r *chartsMCPRec) seen() map[string]url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]url.Values{}
	for k, v := range r.paths {
		out[k] = v
	}
	return out
}

func newChartsMCPServer(t *testing.T, rec *chartsMCPRec) *Server {
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
			_, _ = io.WriteString(w, chartsMCPSeries)
		case "/api/cost/tool-usage":
			_, _ = io.WriteString(w, chartsMCPTools)
		case "/api/cost/failures":
			_, _ = io.WriteString(w, chartsMCPFailures)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}
}

func TestCostChartsToolDeclaration(t *testing.T) {
	tool := (&Server{}).toolCostCharts()
	if tool.Name != "cost_charts" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("cost_charts must be annotated read-only")
	}
	for _, p := range []string{"group_by", "window"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("parameter %q missing", p)
		}
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("all parameters must be optional, required = %v", tool.InputSchema.Required)
	}
	if strings.Contains(strings.ToLower(tool.Description), "prd") {
		t.Errorf("tool text must not say PRD: %s", tool.Description)
	}
}

func TestCostChartsToolIsClassifiedReadOnlyAnalytics(t *testing.T) {
	if got := federation.MCPToolCap["cost_charts"]; got != federation.CapAnalyticsRead {
		t.Errorf("cost_charts capability = %q want %q", got, federation.CapAnalyticsRead)
	}
	if federation.IsConfigWriteTool("cost_charts") || !federation.IsNonConfigWriteTool("cost_charts") {
		t.Error("cost_charts must classify as a non-config-write tool")
	}
}

func TestCostChartsToolReturnsOnePayload(t *testing.T) {
	var rec chartsMCPRec
	s := newChartsMCPServer(t, &rec)

	res, err := s.handleCostCharts(t.Context(), call(map[string]any{"group_by": "model", "window": "7d"}))
	text := resultText(t, res, err)
	if res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	seen := rec.seen()
	for _, path := range []string{"/api/cost/series", "/api/cost/tool-usage", "/api/cost/failures"} {
		if _, ok := seen[path]; !ok {
			t.Fatalf("route %s was not read", path)
		}
		if got := seen[path].Get("window"); got != "7d" {
			t.Errorf("%s window = %q", path, got)
		}
	}
	if seen["/api/cost/series"].Get("group_by") != "model" || seen["/api/cost/tool-usage"].Get("group_by") != "model" {
		t.Errorf("series and tool usage must group by model: %v", seen)
	}
	if got := seen["/api/cost/failures"].Get("group_by"); got != "" {
		t.Errorf("failures must keep its default for model, got %q", got)
	}

	charts, err2 := costreport.ParseCharts([]byte(text))
	if err2 != nil {
		t.Fatalf("result is not a combined payload: %v\n%s", err2, text)
	}
	if charts.GroupBy != "model" || charts.Window != "7d" {
		t.Errorf("request echo = %q/%q", charts.GroupBy, charts.Window)
	}
	if got := charts.ToolUsage.TotalToolCalls; got != 31 {
		t.Errorf("tool calls = %d want 31", got)
	}
	if got := charts.Failures.TotalRetries; got != 5 {
		t.Errorf("retries = %d want 5", got)
	}
	if len(charts.CostOverTime.Groups) != 1 || charts.CostOverTime.Groups[0].Tokens != 4500 {
		t.Errorf("series groups = %+v", charts.CostOverTime.Groups)
	}
	if len(charts.Notes) != 1 || !strings.Contains(charts.Notes[0], "retries and failures are grouped by project") {
		t.Errorf("notes = %v", charts.Notes)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cost_over_time", "tool_usage", "failures"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("payload misses %q", k)
		}
	}
}

func TestCostChartsToolBareForwardsNoQuery(t *testing.T) {
	var rec chartsMCPRec
	s := newChartsMCPServer(t, &rec)
	res, err := s.handleCostCharts(t.Context(), call(nil))
	if text := resultText(t, res, err); res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	for path, q := range rec.seen() {
		if len(q) != 0 {
			t.Errorf("%s: bare call must forward no query, got %v", path, q)
		}
	}
}

func TestCostChartsToolErrors(t *testing.T) {
	var rec chartsMCPRec
	s := newChartsMCPServer(t, &rec)

	res, err := s.handleCostCharts(t.Context(), call(map[string]any{"group_by": "bogus"}))
	if text := resultText(t, res, err); !res.IsError || !strings.Contains(text, "group_by must be one of") {
		t.Errorf("bad group_by: isError=%v %s", res.IsError, text)
	}

	rec.fail = "/api/cost/tool-usage"
	res, err = s.handleCostCharts(t.Context(), call(nil))
	if text := resultText(t, res, err); !res.IsError || !strings.Contains(text, "tool usage") {
		t.Errorf("failing view: isError=%v %s", res.IsError, text)
	}

	// no REST loopback: an error result, not a panic
	res, err = (&Server{}).handleCostCharts(t.Context(), call(nil))
	if text := resultText(t, res, err); !res.IsError || !strings.Contains(text, "cost_charts") {
		t.Errorf("no loopback: isError=%v %s", res.IsError, text)
	}
}
