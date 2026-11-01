package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// shippedCommCall is one captured request the comm verb made against the
// local daemon's loopback HTTP.
type shippedCommCall struct {
	method string
	path   string
	body   map[string]any
}

func TestShippedComm_PrdAndAutomation(t *testing.T) {
	var calls []shippedCommCall
	handler := func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&b)
		}
		calls = append(calls, shippedCommCall{method: r.Method, path: r.URL.Path, body: b})
		w.Write([]byte(`{ "ok": true }`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	// PRD is the default surface.
	calls = nil
	r.HandleTestMessage("autonomous shipped-record p1 commit=c0ffee42 release=v1.2.0 source=backfill")
	if len(calls) != 1 {
		t.Fatalf("PRD shipped: want 1 request, got %d", len(calls))
	}
	c := calls[0]
	if c.method != http.MethodPost || c.path != "/api/autonomous/prds/p1/shipped" {
		t.Fatalf("PRD shipped: want POST /api/autonomous/prds/p1/shipped, got %s %s", c.method, c.path)
	}
	if c.body["merge_commit"] != "c0ffee42" || c.body["merged_in_release"] != "v1.2.0" || c.body["source"] != "backfill" {
		t.Errorf("PRD shipped body = %+v; want commit c0ffee42 release v1.2.0 source backfill", c.body)
	}
	if _, ok := c.body["merged_at"]; ok {
		t.Errorf("PRD shipped body carried an unexpected merged_at: %+v", c.body)
	}

	// --automation=true switches the surface to /api/automations/.
	calls = nil
	r.HandleTestMessage("autonomous shipped-record a1 commit=bead1 release=v3.0.0 source=merge_step automation=true")
	if len(calls) != 1 {
		t.Fatalf("automation shipped: want 1 request, got %d", len(calls))
	}
	c = calls[0]
	if c.method != http.MethodPost || c.path != "/api/automations/a1/shipped" {
		t.Fatalf("automation shipped: want POST /api/automations/a1/shipped, got %s %s", c.method, c.path)
	}
	if c.body["merge_commit"] != "bead1" || c.body["source"] != "merge_step" {
		t.Errorf("automation shipped body = %+v; want commit bead1 source merge_step", c.body)
	}

	// merged_at passes straight through when supplied.
	calls = nil
	r.HandleTestMessage("autonomous shipped-record p1 commit=c1 at=2026-11-20T09:30:00Z")
	if len(calls) != 1 {
		t.Fatalf("shipped at: want 1 request, got %d", len(calls))
	}
	if c := calls[0]; c.body["merged_at"] != "2026-11-20T09:30:00Z" {
		t.Errorf("shipped at body = %+v; want merged_at 2026-11-20T09:30:00Z", c.body)
	}
}

func TestShippedComm_UsageWithoutNetwork(t *testing.T) {
	var calls []shippedCommCall
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, shippedCommCall{method: r.Method, path: r.URL.Path})
		w.Write([]byte(`{ "ok": true }`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	// Missing id -> usage, no daemon request.
	if out := joinReplies(r.HandleTestMessage("autonomous shipped-record")); !strings.Contains(out, "usage") {
		t.Errorf("no-id shipped should print usage, got %q", out)
	}
	if len(calls) != 0 {
		t.Errorf("no-id shipped made %d daemon call(s); want 0", len(calls))
	}

	// Unknown key=value -> usage, no daemon request.
	if out := joinReplies(r.HandleTestMessage("autonomous shipped-record p1 commit=c1 foo=bar")); !strings.Contains(out, "usage") {
		t.Errorf("bad-key shipped should print usage, got %q", out)
	}
	if len(calls) != 0 {
		t.Errorf("bad-key shipped made %d daemon call(s); want 0", len(calls))
	}
}
