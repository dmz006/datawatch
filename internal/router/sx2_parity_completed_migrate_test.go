package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestCompletedMigrateComm_ParsesArgsAndReplies(t *testing.T) {
	var method, path, query string
	n := 0
	handler := func(w http.ResponseWriter, req *http.Request) {
		n++
		method, path, query = req.Method, req.URL.Path, req.URL.RawQuery
		_, _ = w.Write([]byte(`{"considered":4,"would_complete":[{"title":"Zeta merge"}]}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	out := joinReplies(r.HandleTestMessage("autonomous completed-migrate dry_run=true limit=7 id=ab12cd34"))
	if strings.Contains(out, "failed") || !strings.Contains(out, "Zeta merge") {
		t.Fatalf("reply should carry the report, got %q", out)
	}
	if method != http.MethodPost || path != "/api/autonomous/completed/migrate" {
		t.Fatalf("proxied %s %s", method, path)
	}
	if query != "dry_run=true&id=ab12cd34&limit=7" {
		t.Errorf("query = %q", query)
	}

	n = 0
	r.HandleTestMessage("autonomous completed-migrate")
	if n != 1 || query != "" {
		t.Errorf("bare verb: calls=%d query=%q; want 1 call with no query", n, query)
	}
	r.HandleTestMessage("autonomous completed-migrate dry_run=false")
	if strings.Contains(query, "dry_run") {
		t.Errorf("dry_run=false must not be sent, query = %q", query)
	}
}

func TestCompletedMigrateComm_UsageOnBadInput(t *testing.T) {
	n := 0
	handler := func(w http.ResponseWriter, _ *http.Request) {
		n++
		_, _ = w.Write([]byte(`{}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)
	for _, msg := range []string{
		"autonomous completed-migrate please",
		"autonomous completed-migrate limit=abc",
		"autonomous completed-migrate limit=-2",
		"autonomous completed-migrate dry_run=maybe",
		"autonomous completed-migrate bogus=1",
		"autonomous completed-migrate id=",
	} {
		out := joinReplies(r.HandleTestMessage(msg))
		if !strings.Contains(out, "usage") || !strings.Contains(out, "completed-migrate") {
			t.Errorf("%q: want usage, got %q", msg, out)
		}
	}
	if n != 0 {
		t.Errorf("bad input made %d daemon call(s); want 0", n)
	}
}

func TestCompletedMigrateComm_BusyRefusal(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "a final gate is running", http.StatusConflict)
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)
	out := joinReplies(r.HandleTestMessage("autonomous completed-migrate"))
	if !strings.Contains(out, "a final gate is running") || strings.Contains(out, "HTTP 409") {
		t.Errorf("busy reply = %q; want the daemon's plain message", out)
	}
}
