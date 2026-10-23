// comm-channel surfaces: `federation fingerprint list [state]`,
// `federation fingerprint confirm <id>` and
// `federation fingerprint reject <id>` — proxied over the in-process
// loopback to /api/federation/fingerprint-matches.

package router

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// oneMatch is the body of a match record (braces added by each test).
const oneMatch = `"id":"m1","peer":"peer-a","kind":"node","local_name":"node-1","peer_name":"node-remote"`

// matchReplier answers like the daemon's fingerprint-match routes and
// records the request it saw (method + request URI).
func matchReplier(t *testing.T, record string, call *string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if call != nil {
			*call = fmt.Sprintf("%s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(record))
	}
}

// assertReply checks the last captured reply contains every wanted token
// (all tokens in the fingerprint replies are lowercase by construction).
func assertReply(t *testing.T, replies []string, want ...string) {
	t.Helper()
	if len(replies) == 0 {
		t.Fatalf("no reply captured, want one mentioning %v", want)
	}
	last := replies[len(replies)-1]
	for _, w := range want {
		if !strings.Contains(last, w) {
			t.Errorf("reply missing %q:\n%s", w, last)
		}
	}
}

func TestRouterFingerprintList(t *testing.T) {
	t.Run("list all: one line per match plus the pool note", func(t *testing.T) {
		var call string
		_, port := newFakePeerParent(t, matchReplier(t, `{"matches":[{`+oneMatch+`,"state":"pending"}]}`, &call))
		out := setupPeerRouter(t, port).HandleTestMessage("federation fingerprint list")
		if call != "GET /api/federation/fingerprint-matches" {
			t.Fatalf("routed %q, want GET /api/federation/fingerprint-matches", call)
		}
		assertReply(t, out, "m1", "node", "node-1", "peer-a", "node-remote", "pool", "separate")
	})

	t.Run("list confirmed: the state filter is forwarded as a query", func(t *testing.T) {
		var call string
		_, port := newFakePeerParent(t, matchReplier(t, `{"matches":[]}`, &call))
		out := setupPeerRouter(t, port).HandleTestMessage("federation fingerprint list confirmed")
		if call != "GET /api/federation/fingerprint-matches?state=confirmed" {
			t.Fatalf("routed %q, want GET ...?state=confirmed", call)
		}
		assertReply(t, out, "no fingerprint matches")
	})

	t.Run("an unknown state never reaches the daemon", func(t *testing.T) {
		count := 0
		_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
			count++
		})
		out := setupPeerRouter(t, port).HandleTestMessage("federation fingerprint list bogus")
		if count != 0 {
			t.Errorf("daemon hit %d times, want 0 (local validation)", count)
		}
		assertReply(t, out, "pending", "confirmed", "rejected")
	})
}

func TestRouterFingerprintConfirmReject(t *testing.T) {
	t.Run("confirm announces that the pair becomes one pool", func(t *testing.T) {
		var call string
		confirmed := `{` + oneMatch + `,"state":"confirmed"}`
		_, port := newFakePeerParent(t, matchReplier(t, confirmed, &call))
		out := setupPeerRouter(t, port).HandleTestMessage("federation fingerprint confirm m1")
		if call != "POST /api/federation/fingerprint-matches/m1/confirm" {
			t.Fatalf("routed %q, want POST /api/federation/fingerprint-matches/m1/confirm", call)
		}
		assertReply(t, out, "m1", "one shared pool", "node-1", "peer-a", "node-remote")
	})

	t.Run("reject says the pair stays separate (confirm undone)", func(t *testing.T) {
		var call string
		rejected := `{` + oneMatch + `,"state":"rejected"}`
		_, port := newFakePeerParent(t, matchReplier(t, rejected, &call))
		out := setupPeerRouter(t, port).HandleTestMessage("federation fingerprint reject m1")
		if call != "POST /api/federation/fingerprint-matches/m1/reject" {
			t.Fatalf("routed %q, want POST /api/federation/fingerprint-matches/m1/reject", call)
		}
		assertReply(t, out, "m1", "separate resources again", "confirm undone")
	})
}

func TestRouterFingerprintUnknownID(t *testing.T) {
	// The daemon answers 404 "no such match"; the chat reply must carry
	// the failure instead of a canned success.
	var call string
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		call = fmt.Sprintf("%s %s", r.Method, r.URL.RequestURI())
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`no such match`))
	})
	out := setupPeerRouter(t, port).HandleTestMessage("federation fingerprint confirm no-such-id")
	if call != "POST /api/federation/fingerprint-matches/no-such-id/confirm" {
		t.Fatalf("routed %q, want POST .../no-such-id/confirm", call)
	}
	assertReply(t, out, "404", "no such match")
}
