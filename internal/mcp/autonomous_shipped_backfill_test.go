// MCP surface tests for autonomous_shipped_backfill: it proxies one POST to
// the shipped-backfill endpoint (dry_run=true only when asked) and turns a
// daemon refusal into a tool error. No real daemon or home dir is touched —
// a loopback httptest server stands in (same shape as shipped_record_test).

package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestShippedBackfillMCP_ProxiesRun(t *testing.T) {
	var method, path, query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"prds_scanned":4,"prds_marked":3,"prds_already_marked":0,"automations_marked":1,"left_cancelled":1,"unmatched_ids":["deadbeef"]}`))
	})
	res, err := s.handleAutonomousShippedBackfill(context.Background(), callReq(map[string]any{
		"dry_run": false,
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/shipped/backfill" {
		t.Fatalf("proxied %s %s; want POST /api/autonomous/shipped/backfill", method, path)
	}
	// An explicit dry_run=false runs the writing pass.
	if query == "dry_run=true" {
		t.Errorf("dry_run=false must not send dry_run=true, query = %q", query)
	}
	if out := resContentText(t, res); !strings.Contains(out, "deadbeef") {
		t.Errorf("tool result = %q; want the daemon's report", out)
	}
}

func TestShippedBackfillMCP_ProxiesDryRun(t *testing.T) {
	var method, path, query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"prds_scanned":4,"prds_marked":0,"prds_already_marked":0,"automations_marked":0,"left_cancelled":1,"unmatched_ids":["deadbeef"]}`))
	})
	res, err := s.handleAutonomousShippedBackfill(context.Background(), callReq(map[string]any{
		"dry_run": true,
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/shipped/backfill" {
		t.Fatalf("proxied %s %s; want POST /api/autonomous/shipped/backfill", method, path)
	}
	if query != "dry_run=true" {
		t.Errorf("dry_run=true: query = %q, want dry_run=true", query)
	}
}

func TestShippedBackfillMCP_DryRunDefaultsOff(t *testing.T) {
	var query string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"prds_marked":1}`))
	})
	res, err := s.handleAutonomousShippedBackfill(context.Background(), callReq(map[string]any{}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if query == "dry_run=true" {
		t.Errorf("absent dry_run must default to the writing pass, query = %q", query)
	}
}

func TestShippedBackfillMCP_RefusalIsAToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no base repository configured", http.StatusBadRequest)
	})
	res, err := s.handleAutonomousShippedBackfill(context.Background(), callReq(map[string]any{}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must come back as a tool error: err=%v res=%+v", err, res)
	}
}

func TestShippedBackfillMCP_ToolShape(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousShippedBackfill()
	if tool.Name != "autonomous_shipped_backfill" {
		t.Fatalf("name = %q", tool.Name)
	}
	if _, ok := tool.InputSchema.Properties["dry_run"]; !ok {
		t.Errorf("tool is missing the dry_run parameter: %+v", tool.InputSchema.Properties)
	}
	for _, req := range tool.InputSchema.Required {
		t.Errorf("no parameter of autonomous_shipped_backfill may be required, got %q", req)
	}
}
