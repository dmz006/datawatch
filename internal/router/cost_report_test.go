// `cost report` comm-channel command — Sprint Sx2 parity tests. The verb
// must reach the same /api/cost/report engine endpoint as the CLI and
// REST surface (no copied logic), forward group_by / since / until as
// query parameters, and keep bare "cost" / "cost <id>" parsing intact.

package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const costReportCommFakeBody = `{
  "group_by": "backend",
  "rows": [
    {"key": "anthropic", "sessions": 4, "tokens_in": 120000, "tokens_out": 8000, "usd_estimate": 1.25}
  ],
  "retention_days": 7,
  "lifetime_fallback": false
}`

type commRecQuery struct {
	mu   sync.Mutex
	path string
	vals url.Values
}

func (r *commRecQuery) set(path string, q url.Values) {
	r.mu.Lock()
	r.path, r.vals = path, q
	r.mu.Unlock()
}

func (r *commRecQuery) values() (string, url.Values) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.path, r.vals
}

func newCostReportCommDaemon(t *testing.T, rec *commRecQuery) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.set(req.URL.Path, req.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, costReportCommFakeBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCostReport_Parse_Bare(t *testing.T) {
	c := Parse("cost report")
	if c.Type != CmdCostReport || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestCostReport_Parse_Tokens(t *testing.T) {
	c := Parse("cost report group_by=backend since=2026-01-01 until=2026-01-08")
	if c.Type != CmdCostReport {
		t.Fatalf("type=%v", c.Type)
	}
	if c.Text != "group_by=backend since=2026-01-01 until=2026-01-08" {
		t.Errorf("text=%q", c.Text)
	}
}

func TestCostReport_Parse_CostIDUnchanged(t *testing.T) {
	// Plain "cost" and "cost <full_id>" must still be the rollup command —
	// the new verb must not swallow them.
	if c := Parse("cost"); c.Type != CmdCost || c.Text != "" {
		t.Errorf("bare cost = %+v", c)
	}
	if c := Parse("cost host-aa01"); c.Type != CmdCost || c.Text != "host-aa01" {
		t.Errorf("cost <id> = %+v", c)
	}
	// Words merely starting with "report" are not the verb either.
	if c := Parse("cost reported"); c.Type != CmdCost {
		t.Errorf("cost reported = %+v (must stay the cost command)", c)
	}
}

func TestCostReport_Command_Dispatch(t *testing.T) {
	var rec commRecQuery
	srv := newCostReportCommDaemon(t, &rec)

	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("cost report group_by=backend since=2026-01-01T00:00:00Z until=2026-01-08T00:00:00Z"), "\n")

	path, q := rec.values()
	if q == nil {
		t.Fatalf("daemon never saw a request; reply:\n%s", out)
	}
	if path != "/api/cost/report" {
		t.Errorf("path = %q want /api/cost/report", path)
	}
	if got := q.Get("group_by"); got != "backend" {
		t.Errorf("group_by = %q want backend", got)
	}
	if got := q.Get("since"); got != "2026-01-01T00:00:00Z" {
		t.Errorf("since = %q", got)
	}
	if got := q.Get("until"); got != "2026-01-08T00:00:00Z" {
		t.Errorf("until = %q", got)
	}
	for _, frag := range []string{"cost report", "anthropic: 4 sessions", "$1.25"} {
		if !strings.Contains(out, frag) {
			t.Errorf("reply misses %q:\n%s", frag, out)
		}
	}
	if strings.Contains(out, "failed") {
		t.Errorf("reply reports failure:\n%s", out)
	}
}

func TestCostReport_Command_BareForwardsNoQuery(t *testing.T) {
	var rec commRecQuery
	srv := newCostReportCommDaemon(t, &rec)

	r := loopbackRouter(t, srv)
	_ = r.HandleTestMessage("cost report")

	_, q := rec.values()
	if q == nil {
		t.Fatal("daemon never saw a request")
	}
	if len(q) != 0 {
		t.Errorf("bare verb must forward no query params, got %v", q)
	}
}

func TestCostReport_Command_UnknownToken(t *testing.T) {
	r := &Router{hostname: "h"}
	out := strings.Join(r.HandleTestMessage("cost report bogus=1"), "\n")
	if !strings.Contains(out, "usage: cost report") {
		t.Errorf("expected usage hint, got:\n%s", out)
	}
	out = strings.Join(r.HandleTestMessage("cost report notakey"), "\n")
	if !strings.Contains(out, "usage: cost report") {
		t.Errorf("expected usage hint for non key=value token, got:\n%s", out)
	}
}

func TestCostReport_HelpListsVerb(t *testing.T) {
	r := &Router{hostname: "h"}
	out := strings.Join(r.HandleTestMessage("help"), "\n")
	if !strings.Contains(out, "cost report [group_by=") {
		t.Errorf("help misses the verb:\n%s", out[:min(2000, len(out))])
	}
}
