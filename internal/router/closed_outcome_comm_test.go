package router

import (
	"encoding/json"
	"net/http"
	"testing"
)

type outcomeCommCall struct {
	method, path, query string
	body                map[string]any
}

func TestClosedOutcomeComm(t *testing.T) {
	var calls []outcomeCommCall
	handler := func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&b)
		}
		calls = append(calls, outcomeCommCall{r.Method, r.URL.Path, r.URL.RawQuery, b})
		_, _ = w.Write([]byte(`{"mode":"shadow","scanned":2,"counts":{"closed_no_outcome":1},"findings":[{"automaton":"ab12","kind":"closed_no_outcome","detail":"archived with no outcome"}]}`))
	}
	_, port := newFakePeerParent(t, handler)
	r := setupRetryRouter(t, port)

	cases := []struct {
		msg      string
		method   string
		path     string
		query    string
		wantBody map[string]any
	}{
		{"autonomous outcome ab12 outcome=delivered via=ff00 note=the twin shipped it", http.MethodPost, "/api/autonomous/prds/ab12/outcome", "",
			map[string]any{"outcome": "delivered", "delivered_via": "ff00", "note": "the twin shipped it"}},
		{"autonomous outcome ab12 outcome=dropped archive=true", http.MethodPost, "/api/autonomous/prds/ab12/archive", "",
			map[string]any{"outcome": "dropped"}},
		{"autonomous outcome-check", http.MethodGet, "/api/autonomous/outcome-check", "", nil},
		{"autonomous outcome-check run=true", http.MethodPost, "/api/autonomous/outcome-check", "", nil},
		{"autonomous deploy-record release=v9.1.0 host=h1 health=passed detail=restart ok", http.MethodPost, "/api/autonomous/deploy/record", "",
			map[string]any{"release": "v9.1.0", "host": "h1", "health": "passed", "detail": "restart ok"}},
		{"autonomous cancel ab12", http.MethodDelete, "/api/autonomous/prds/ab12", "", nil},
		{"autonomous cancel ab12 outcome=superseded via=ff00", http.MethodDelete, "/api/autonomous/prds/ab12", "delivered_via=ff00&outcome=superseded", nil},
	}
	for _, tc := range cases {
		calls = nil
		r.HandleTestMessage(tc.msg)
		if len(calls) != 1 {
			t.Fatalf("%q: want 1 request, got %d", tc.msg, len(calls))
		}
		c := calls[0]
		if c.method != tc.method || c.path != tc.path || c.query != tc.query {
			t.Errorf("%q: got %s %s ?%s, want %s %s ?%s", tc.msg, c.method, c.path, c.query, tc.method, tc.path, tc.query)
		}
		for k, v := range tc.wantBody {
			if c.body[k] != v {
				t.Errorf("%q: body[%s] = %v, want %v", tc.msg, k, c.body[k], v)
			}
		}
	}

	// Bad input sends nothing.
	for _, msg := range []string{"autonomous outcome", "autonomous outcome ab12", "autonomous deploy-record", "autonomous outcome ab12 junk"} {
		calls = nil
		r.HandleTestMessage(msg)
		if len(calls) != 0 {
			t.Errorf("%q must not call the daemon, got %d", msg, len(calls))
		}
	}
}

func TestParseOutcomeTokensNoteTakesTheRest(t *testing.T) {
	kv, ok := parseOutcomeTokens([]string{"outcome=dropped", "note=a", "b", "c=d"})
	if !ok || kv["outcome"] != "dropped" || kv["note"] != "a b c=d" {
		t.Fatalf("kv=%v ok=%v", kv, ok)
	}
	if _, ok := parseOutcomeTokens([]string{"nokey"}); ok {
		t.Fatal("a token with no = is malformed")
	}
}
