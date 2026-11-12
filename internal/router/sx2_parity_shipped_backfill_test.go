package router

import (
	"net/http"
	"strings"
	"testing"
)

// shippedBackfillCommCall is one captured request the shipped-backfill
// comm verb made against the local daemon's loopback HTTP.
type shippedBackfillCommCall struct {
	method string
	path   string
	query  string
}

func TestShippedBackfillComm_DefaultWrites(t *testing.T) {
	var calls []shippedBackfillCommCall
	handler := func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, shippedBackfillCommCall{method: req.Method, path: req.URL.Path, query: req.URL.RawQuery})
		_, _ = w.Write([]byte(`{"prds_marked":3,"prds_already_marked":0,"automations_marked":1,"left_cancelled":1,"unmatched_ids":["deadbeef"]}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	out := joinReplies(r.HandleTestMessage("autonomous shipped-backfill"))
	if strings.Contains(out, "failed") {
		t.Fatalf("shipped-backfill verb failed: %q", out)
	}
	if !strings.Contains(out, "deadbeef") {
		t.Errorf("reply should carry the report, got %q", out)
	}
	if len(calls) != 1 {
		t.Fatalf("want 1 daemon request, got %d", len(calls))
	}
	c := calls[0]
	if c.method != http.MethodPost || c.path != "/api/autonomous/shipped/backfill" {
		t.Fatalf("proxied %s %s; want POST /api/autonomous/shipped/backfill", c.method, c.path)
	}
	// With no flags the daemon's writing pass is the default.
	if c.query == "dry_run=true" {
		t.Errorf("flagless invocation: query = %q, must not be dry_run=true", c.query)
	}
}

func TestShippedBackfillComm_DryRunAndRefusals(t *testing.T) {
	var calls []shippedBackfillCommCall
	handler := func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, shippedBackfillCommCall{
			method: req.Method,
			path:   req.URL.Path,
			query:  req.URL.RawQuery,
		})
		_, _ = w.Write([]byte(`{"prds_marked":0}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	// dry_run=true flags the daemon's scan-only pass.
	calls = nil
	r.HandleTestMessage("autonomous shipped-backfill dry_run=true")
	if len(calls) != 1 {
		t.Fatalf("dry_run=true: want 1 request, got %d", len(calls))
	}
	c := calls[0]
	if c.method != http.MethodPost || c.path != "/api/autonomous/shipped/backfill" {
		t.Fatalf("proxied %s %s; want POST /api/autonomous/shipped/backfill", c.method, c.path)
	}
	if c.query != "dry_run=true" {
		t.Errorf("dry_run=true: query = %q, want dry_run=true", c.query)
	}

	// dry_run=false is the default: no dry_run flag on the daemon side.
	calls = nil
	r.HandleTestMessage("autonomous shipped-backfill dry_run=false")
	if len(calls) != 1 {
		t.Fatalf("dry_run=false: want 1 request, got %d", len(calls))
	}
	if c := calls[0]; c.query == "dry_run=true" {
		t.Errorf("dry_run=false must not send dry_run=true, query = %q", c.query)
	}

	// A bare word and an unknown key both print usage without a request.
	calls = nil
	if out := joinReplies(r.HandleTestMessage("autonomous shipped-backfill please")); !strings.Contains(out, "usage") {
		t.Errorf("bare word should print usage, got %q", out)
	}
	if len(calls) != 0 {
		t.Errorf("bare-word invocation made %d daemon call(s); want 0", len(calls))
	}

	calls = nil
	if out := joinReplies(r.HandleTestMessage("autonomous shipped-backfill foo=bar")); !strings.Contains(out, "usage") {
		t.Errorf("unknown key should print usage, got %q", out)
	}
	if len(calls) != 0 {
		t.Errorf("unknown-key invocation made %d daemon call(s); want 0", len(calls))
	}
}
