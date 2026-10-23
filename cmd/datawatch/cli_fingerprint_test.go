// CLI surfaces: `federation fingerprint list [--state ...]`,
// `federation fingerprint confirm <id>` and
// `federation fingerprint reject <id>` — thin proxies over the daemon's
// /api/federation/fingerprint-matches routes, with the pool/undo semantics
// spelled out in the output.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fingerprintMatchCall struct {
	method string
	path   string
	query  string
}

// fpMatchDaemon stands in for the daemon: records each request's method,
// path and raw query string and answers with the match payloads below.
func fpMatchDaemon(t *testing.T) func() []fingerprintMatchCall {
	t.Helper()
	var mu sync.Mutex
	var calls []fingerprintMatchCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, fingerprintMatchCall{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/confirm"):
			_, _ = w.Write([]byte(`{"id":"m1","peer":"peer-a","kind":"node","local_name":"node-1","peer_name":"node-remote","state":"confirmed"}`))
		case strings.HasSuffix(r.URL.Path, "/reject"):
			_, _ = w.Write([]byte(`{"id":"m1","peer":"peer-a","kind":"node","local_name":"node-1","peer_name":"node-remote","state":"rejected"}`))
		default:
			matches, _ := json.Marshal([]map[string]any{
				{"id": "m1", "peer": "peer-a", "kind": "node", "local_name": "node-1", "peer_name": "node-remote", "state": "pending"},
				{"id": "m2", "peer": "peer-b", "kind": "billing", "local_name": "plan-x", "peer_name": "plan-x", "state": "confirmed"},
			})
			_, _ = w.Write(append(append([]byte(`{"matches":`), matches...), '}'))
		}
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return func() []fingerprintMatchCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]fingerprintMatchCall(nil), calls...)
	}
}

func runFingerprintArgs(t *testing.T, args ...string) string {
	t.Helper()
	c := newFederationCmd()
	c.SetArgs(args)
	c.SilenceUsage = true
	c.SilenceErrors = true
	return captureStdout(t, func() { _ = runRunE(t, c) })
}

func TestCLIFingerprintList(t *testing.T) {
	t.Run("lists all matches, one line per match, with the pool note", func(t *testing.T) {
		seen := fpMatchDaemon(t)
		out := runFingerprintArgs(t, "fingerprint", "list")
		calls := seen()
		if len(calls) != 1 || calls[0].method != http.MethodGet ||
			calls[0].path != "/api/federation/fingerprint-matches" || calls[0].query != "" {
			t.Fatalf("routed %+v, want one GET /api/federation/fingerprint-matches with no query", calls)
		}
		for _, want := range []string{"m1", "pending", "node", "node-1", "peer-a", "node-remote", "m2", "confirmed", "pool", "separate"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("--state is forwarded as the query filter", func(t *testing.T) {
		seen := fpMatchDaemon(t)
		runFingerprintArgs(t, "fingerprint", "list", "--state", "confirmed")
		calls := seen()
		if len(calls) != 1 || calls[0].query != "state=confirmed" {
			t.Fatalf("routed %+v, want GET with query state=confirmed", calls)
		}
	})

	t.Run("unknown --state is rejected before any daemon call", func(t *testing.T) {
		seen := fpMatchDaemon(t)
		c := newFederationCmd()
		c.SetArgs([]string{"fingerprint", "list", "--state", "bogus"})
		c.SilenceUsage = true
		c.SilenceErrors = true
		err := c.Execute()
		if err == nil || !strings.Contains(err.Error(), "pending, confirmed or rejected") {
			t.Fatalf("err = %v, want a message naming the valid states", err)
		}
		if len(seen()) != 0 {
			t.Errorf("daemon hit %d times, want 0 (local validation)", len(seen()))
		}
	})

	t.Run("empty result says so explicitly", func(t *testing.T) {
		prev := serverURL
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"matches":[]}`))
		}))
		serverURL = srv.URL
		t.Cleanup(func() {
			serverURL = prev
			srv.Close()
		})
		c := newFederationCmd()
		c.SetArgs([]string{"fingerprint", "list"})
		c.SilenceUsage = true
		c.SilenceErrors = true
		out := captureStdout(t, func() { _ = c.Execute() })
		if !strings.Contains(out, "no fingerprint matches") {
			t.Errorf("output = %q, want an explicit empty message", out)
		}
	})
}

func TestCLIFingerprintConfirmReject(t *testing.T) {
	t.Run("confirm posts to {id}/confirm and announces the shared pool", func(t *testing.T) {
		seen := fpMatchDaemon(t)
		out := runFingerprintArgs(t, "fingerprint", "confirm", "m1")
		calls := seen()
		if len(calls) != 1 || calls[0].method != http.MethodPost ||
			calls[0].path != "/api/federation/fingerprint-matches/m1/confirm" {
			t.Fatalf("routed %+v, want POST /api/federation/fingerprint-matches/m1/confirm", calls)
		}
		for _, want := range []string{"m1", "one shared pool", "node-1", "peer-a", "node-remote"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("reject posts to {id}/reject and says the resources stay separate", func(t *testing.T) {
		seen := fpMatchDaemon(t)
		out := runFingerprintArgs(t, "fingerprint", "reject", "m1")
		calls := seen()
		if len(calls) != 1 || calls[0].method != http.MethodPost ||
			calls[0].path != "/api/federation/fingerprint-matches/m1/reject" {
			t.Fatalf("routed %+v, want POST /api/federation/fingerprint-matches/m1/reject", calls)
		}
		for _, want := range []string{"m1", "separate resources again", "confirm undone"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("a missing id errors before any daemon call", func(t *testing.T) {
		seen := fpMatchDaemon(t)
		for _, verb := range []string{"confirm", "reject"} {
			c := newFederationCmd()
			c.SetArgs([]string{"fingerprint", verb})
			c.SilenceUsage = true
			c.SilenceErrors = true
			if err := c.Execute(); err == nil {
				t.Errorf("%s without an id should fail", verb)
			}
		}
		if len(seen()) != 0 {
			t.Errorf("daemon hit %d times, want 0", len(seen()))
		}
	})
}
