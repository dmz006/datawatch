// Tests for the shipped-state MCP read tools (shipped.go): shipped_counts
// proxies the counts endpoint, and prd_list / automation_list forward their
// shipped / status selections to the daemon REST endpoints. All against an
// in-memory loopback proxy — no home directory, no daemon.

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// shipCall records one proxied request.
type shipCall struct {
	method string
	path   string
	query  string
}

// shipCountsHandler stands in for the daemon and returns a canned counts
// payload regardless of which list endpoint is hit.
func shipCountsHandler(calls *[]shipCall) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, shipCall{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"prds":{"running":1,"completed":1,"shipped":2},"automations":{"active":1,"shipped":1},"settings":{"enabled":true,"show_chip":true,"filter_default":"all","count_separately":true}}`))
	}
}

// TestShippedMCP_CountsProxiesGET: autonomous_shipped_counts is a read tool —
// no parameters, no body, exactly the daemon's counts endpoint.
func TestShippedMCP_CountsProxiesGET(t *testing.T) {
	var calls []shipCall
	s := capacityTestServer(t, shipCountsHandler(&calls))
	res, err := s.handleAutonomousShippedCounts(context.Background(), callReq(nil))
	if err != nil {
		t.Fatalf("handleAutonomousShippedCounts: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("proxy calls = %d (%+v); want 1", len(calls), calls)
	}
	c := calls[0]
	if c.method != http.MethodGet || c.path != "/api/autonomous/shipped/counts" || c.query != "" {
		t.Errorf("proxied %s %s?%s; want GET /api/autonomous/shipped/counts with no query", c.method, c.path, c.query)
	}
	for _, want := range []string{"\"prds\"", "\"automations\"", "\"shipped\":2", "\"count_separately\":true"} {
		if !strings.Contains(resContentText(t, res), want) {
			t.Errorf("result %q missing %q", resContentText(t, res), want)
		}
	}
}

// TestShippedMCP_PrdListShippedForwarded: the shipped flag (true/false) is
// forwarded to the daemon as the shipped query, a rejected value never is.
func TestShippedMCP_PrdListShippedForwarded(t *testing.T) {
	var calls []shipCall
	s := capacityTestServer(t, shipCountsHandler(&calls))

	for _, shipped := range []string{"true", "false"} {
		calls = nil
		if _, err := s.handleAutonomousPRDList(context.Background(), callReq(map[string]any{"shipped": shipped})); err != nil {
			t.Fatalf("shipped=%s: %v", shipped, err)
		}
		if len(calls) != 1 {
			t.Fatalf("shipped=%s: proxy calls = %d (%+v); want 1", shipped, len(calls), calls)
		}
		c := calls[0]
		if c.path != "/api/autonomous/prds" || c.query != "shipped="+shipped {
			t.Errorf("shipped=%s proxied %s?%s; want /api/autonomous/prds?shipped=%s", shipped, c.path, c.query, shipped)
		}
	}

	calls = nil
	res, err := s.handleAutonomousPRDList(context.Background(), callReq(map[string]any{"shipped": "banana"}))
	if err != nil {
		t.Fatalf("bogus shipped: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("bogus shipped still proxied: %+v", calls)
	}
	if res == nil || !strings.Contains(resContentText(t, res), "shipped") {
		t.Errorf("bogus shipped: no error text about shipped (%v)", res)
	}
}

// TestShippedMCP_PrdListStatusShippedForwarded: the status selection may
// carry the shipped bucket and is forwarded verbatim; no selection at all
// falls back to the full list (full=1).
func TestShippedMCP_PrdListStatusShippedForwarded(t *testing.T) {
	var calls []shipCall
	s := capacityTestServer(t, shipCountsHandler(&calls))

	calls = nil
	if _, err := s.handleAutonomousPRDList(context.Background(), callReq(map[string]any{"status": "shipped"})); err != nil {
		t.Fatalf("status=shipped: %v", err)
	}
	if len(calls) != 1 || calls[0].path != "/api/autonomous/prds" || calls[0].query != "status=shipped" {
		t.Errorf("status=shipped proxied %+v; want /api/autonomous/prds?status=shipped", calls)
	}

	calls = nil
	if _, err := s.handleAutonomousPRDList(context.Background(), callReq(nil)); err != nil {
		t.Fatalf("no selection: %v", err)
	}
	if len(calls) != 1 || calls[0].query != "full=1" {
		t.Errorf("no selection proxied %+v; want /api/autonomous/prds?full=1", calls)
	}
}

// TestShippedMCP_AutomationListShippedForwarded: the automation list's
// shipped flag joins the path as the daemon's shipped query; an "all"
// selection means the daemon's default view (no query).
func TestShippedMCP_AutomationListShippedForwarded(t *testing.T) {
	var calls []shipCall
	s := capacityTestServer(t, shipCountsHandler(&calls))

	calls = nil
	res, err := s.handleAutomationList(context.Background(), callReq(map[string]any{"shipped": "true"}))
	if err != nil {
		t.Fatalf("shipped=true: %v", err)
	}
	if len(calls) != 1 || calls[0].method != http.MethodGet || calls[0].path != "/api/automations" || calls[0].query != "shipped=true" {
		t.Errorf("shipped=true proxied %+v; want GET /api/automations?shipped=true", calls)
	}
	if res == nil || resContentText(t, res) == "" {
		t.Errorf("empty result")
	}

	calls = nil
	if _, err := s.handleAutomationList(context.Background(), callReq(map[string]any{"shipped": "all"})); err != nil {
		t.Fatalf("shipped=all: %v", err)
	}
	if len(calls) != 1 || calls[0].query != "" {
		t.Errorf("shipped=all proxied %+v; want /api/automations with no query (the daemon's default view)", calls)
	}

	calls = nil
	res, err = s.handleAutomationList(context.Background(), callReq(map[string]any{"shipped": "banana"}))
	if err != nil {
		t.Fatalf("bogus shipped: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("bogus shipped still proxied: %+v", calls)
	}
	if res == nil || !strings.Contains(resContentText(t, res), "shipped") {
		t.Errorf("bogus shipped: no error text about shipped (%v)", res)
	}
}
