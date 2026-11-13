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
		_, _ = w.Write([]byte(`{ "ok": true }`))
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
		_, _ = w.Write([]byte(`{ "ok": true }`))
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

// TestShippedComm_ShippedCounts: the shipped-counts verb proxies the daemon's
// per-status counts endpoint and echoes the buckets back on the comm channel.
func TestShippedComm_ShippedCounts(t *testing.T) {
	var calls []shippedCommCall
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, shippedCommCall{method: r.Method, path: r.URL.Path})
		if r.Method != http.MethodGet || r.URL.Path != "/api/autonomous/shipped/counts" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte(`{"prds":{"running":1,"completed":1,"shipped":2},"automations":{"active":1,"shipped":1},"settings":{"enabled":true,"show_chip":true,"filter_default":"all","count_separately":true}}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	out := joinReplies(r.HandleTestMessage("autonomous shipped-counts"))
	if len(calls) != 1 || calls[0].method != http.MethodGet || calls[0].path != "/api/autonomous/shipped/counts" {
		t.Fatalf("shipped-counts: want GET /api/autonomous/shipped/counts, got %+v", calls)
	}
	for _, want := range []string{"autonomous shipped-counts", "\"running\": 1", "\"shipped\": 2", "\"active\": 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("reply missing %q: %q", want, out)
		}
	}
}

// TestShippedComm_ListShippedSelection: the list verb forwards a shipped
// selection (and a status bucket that may include shipped) to the daemon as
// query parameters; with no selection it reads the summary list. A shipped
// value that is neither true nor false is refused before anything is sent.
func TestShippedComm_ListShippedSelection(t *testing.T) {
	var uris []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/autonomous/prds" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		uris = append(uris, r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"prds":[{"id":"prd-1","status":"running"}],"total":1}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	if out := joinReplies(r.HandleTestMessage("autonomous list shipped=true")); !strings.Contains(out, "autonomous list") {
		t.Fatalf("shipped=true reply: %q", out)
	}
	if out := joinReplies(r.HandleTestMessage("autonomous list shipped=false")); !strings.Contains(out, "autonomous list") {
		t.Fatalf("shipped=false reply: %q", out)
	}
	if out := joinReplies(r.HandleTestMessage("autonomous list status=shipped")); !strings.Contains(out, "autonomous list") {
		t.Fatalf("status=shipped reply: %q", out)
	}
	if out := joinReplies(r.HandleTestMessage("autonomous list")); !strings.Contains(out, "autonomous list") {
		t.Fatalf("no-filter reply: %q", out)
	}
	if want := []string{"/api/autonomous/prds?shipped=true", "/api/autonomous/prds?shipped=false", "/api/autonomous/prds?status=shipped", "/api/autonomous/prds"}; len(uris) != len(want) {
		t.Fatalf("daemon requests = %v; want %v", uris, want)
	} else {
		for i := range want {
			if uris[i] != want[i] {
				t.Errorf("request %d = %q; want %q", i, uris[i], want[i])
			}
		}
	}

	// shipped=banana -> usage, nothing proxied.
	before := len(uris)
	if out := joinReplies(r.HandleTestMessage("autonomous list shipped=banana")); !strings.Contains(out, "usage") {
		t.Errorf("bogus shipped should print usage, got %q", out)
	}
	if len(uris) != before {
		t.Errorf("bogus shipped made %d extra daemon call(s); want 0", len(uris)-before)
	}
}

// TestAutonomousListFull_CommVerbSummary: the list verb never sends full (chat
// must not receive whole records) and forwards limit and cursor; a bad limit
// is refused before anything is sent.
func TestAutonomousListFull_CommVerbSummary(t *testing.T) {
	var uris []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"prds":[],"total":0}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	joinReplies(r.HandleTestMessage("autonomous list"))
	joinReplies(r.HandleTestMessage("autonomous list limit=5 cursor=abc full=true"))
	if len(uris) != 1 || uris[0] != "/api/autonomous/prds" {
		t.Fatalf("requests = %v; want one bare summary list (full=true is not a verb argument)", uris)
	}
	joinReplies(r.HandleTestMessage("autonomous list limit=5 cursor=abc"))
	if len(uris) != 2 || uris[1] != "/api/autonomous/prds?cursor=abc&limit=5" {
		t.Fatalf("requests = %v; want paged summary list", uris)
	}
	if out := joinReplies(r.HandleTestMessage("autonomous list limit=0")); !strings.Contains(out, "usage") || len(uris) != 2 {
		t.Errorf("limit=0 should print usage and send nothing, got %q (%d requests)", out, len(uris))
	}
}
