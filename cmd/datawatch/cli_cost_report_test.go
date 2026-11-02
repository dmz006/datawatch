// `datawatch cost report` — Sprint Sx parity CLI tests. The CLI is a thin
// REST proxy for GET /api/cost/report, so these tests point the CLI at a
// fake daemon (via the --url serverURL override), pin the query-parameter
// forwarding, the table rendering (usd cell only where the server sends
// one, retention + lifetime-fallback footer), and the JSON format.

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

// costReportFakeBody mirrors a costreport.Report: anthropic has a
// usd_estimate, ollama does not (no rates configured for that backend).
const costReportFakeBody = `{
  "group_by": "backend",
  "rows": [
    {"key": "anthropic", "sessions": 4, "tokens_in": 120000, "tokens_out": 8000, "usd_estimate": 1.25},
    {"key": "ollama", "sessions": 2, "tokens_in": 90000, "tokens_out": 1000}
  ],
  "retention_days": 7,
  "lifetime_fallback": false
}`

type recQuery struct {
	mu   sync.Mutex
	vals url.Values
}

func (r *recQuery) set(q url.Values) {
	r.mu.Lock()
	r.vals = q
	r.mu.Unlock()
}

// newCostReportFakeDaemon fakes the daemon's /api/cost/report and records
// the last query values it saw.
func newCostReportFakeDaemon(t *testing.T, rec *recQuery) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/cost/report" {
			t.Errorf("path = %q want /api/cost/report", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		rec.set(req.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, costReportFakeBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func lastQuery(t *testing.T, rec *recQuery) url.Values {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.vals == nil {
		t.Fatal("fake daemon never saw a request")
	}
	return rec.vals
}

// withServerURL runs fn against the fake daemon by pointing the CLI's
// --url override at it (daemonURL honors serverURL first).
func withServerURL(t *testing.T, srv *httptest.Server, fn func() error) error {
	t.Helper()
	old := serverURL
	serverURL = srv.URL
	defer func() { serverURL = old }()
	return fn()
}

func TestCostReport_CLI_Flags(t *testing.T) {
	c := newCostReportCmd()
	if c.Name() != "report" {
		t.Errorf("name = %q want report", c.Name())
	}
	for _, f := range []string{"group-by", "since", "until", "format"} {
		if c.Flag(f) == nil {
			t.Errorf("--%s flag missing", f)
		}
	}
}

func TestCostReport_CostParentIncludesReport(t *testing.T) {
	c := newCostCmd()
	want := map[string]bool{"summary": false, "usage": false, "rates": false, "report": false}
	for _, sub := range c.Commands() {
		want[sub.Name()] = true
	}
	for sub, ok := range want {
		if !ok {
			t.Errorf("cost missing subcommand: %s", sub)
		}
	}
}

func TestCostReport_CLI_TableOutput(t *testing.T) {
	var rec recQuery
	srv := newCostReportFakeDaemon(t, &rec)

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	var runErr error
	err = withServerURL(t, srv, func() error {
		os.Stdout = writer
		c := newCostReportCmd()
		runErr = c.RunE(c, []string{})
		if closeErr := writer.Close(); closeErr != nil {
			t.Errorf("writer.Close: %v", closeErr)
		}
		os.Stdout = oldStdout
		return runErr
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if runErr != nil {
		t.Fatalf("runErr: %v", runErr)
	}
	capBuf, _ := io.ReadAll(reader)
	out := string(capBuf)

	for _, frag := range []string{
		"anthropic", "120000", "8000", "$1.25",
		"ollama", "90000", "1000", // row without usd still prints tokens
		"usd", // header column present (anthropic has one)
		"retention: 7 days",
		"lifetime fallback: no",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("table output misses %q:\n%s", frag, out)
		}
	}
	if strings.Contains(out, "lifetime fallback: yes") {
		t.Errorf("lifetime fallback flag wrong:\n%s", out)
	}
}

func TestCostReport_CLI_ForwardsQueryParams(t *testing.T) {
	var rec recQuery
	srv := newCostReportFakeDaemon(t, &rec)

	oldStdout := os.Stdout
	rw, ww, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	err = withServerURL(t, srv, func() error {
		os.Stdout = ww
		var runErr error
		defer func() { os.Stdout = oldStdout }()
		c := newCostReportCmd()
		for flag, val := range map[string]string{"group-by": "backend", "since": "2026-01-01", "until": "2026-01-08"} {
			if serr := c.Flags().Set(flag, val); serr != nil {
				return serr
			}
		}
		runErr = c.RunE(c, []string{})
		if closeErr := ww.Close(); closeErr != nil {
			t.Errorf("writer.Close: %v", closeErr)
		}
		return runErr
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	_, _ = io.ReadAll(rw)

	q := lastQuery(t, &rec)
	if got := q.Get("group_by"); got != "backend" {
		t.Errorf("group_by = %q want backend", got)
	}
	if got := q.Get("since"); got != "2026-01-01" {
		t.Errorf("since = %q want 2026-01-01", got)
	}
	if got := q.Get("until"); got != "2026-01-08" {
		t.Errorf("until = %q want 2026-01-08", got)
	}
}

func TestCostReport_CLI_JSONFormatPassthrough(t *testing.T) {
	var rec recQuery
	srv := newCostReportFakeDaemon(t, &rec)

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	rerr := withServerURL(t, srv, func() error {
		os.Stdout = writer
		var runErr error
		defer func() { os.Stdout = oldStdout }()
		c := newCostReportCmd()
		if serr := c.Flags().Set("format", "json"); serr != nil {
			return serr
		}
		if serr := c.Flags().Set("group-by", "backend"); serr != nil {
			return serr
		}
		runErr = c.RunE(c, []string{})
		if closeErr := writer.Close(); closeErr != nil {
			t.Errorf("writer.Close: %v", closeErr)
		}
		return runErr
	})
	if rerr != nil {
		t.Fatalf("run: %v", rerr)
	}
	capBuf, _ := io.ReadAll(reader)
	out := string(capBuf)

	// --format json routes through prettyPrint: raw JSON, not the table.
	if !strings.Contains(out, `"group_by": "backend"`) && !strings.Contains(out, `"group_by":"backend"`) {
		t.Errorf("json format should pass the report body through:\n%s", out)
	}
	if strings.Contains(out, "retention: 7 days") {
		t.Errorf("json format must not add the text footer:\n%s", out)
	}
	if q := lastQuery(t, &rec); q.Get("group_by") != "backend" {
		t.Errorf("group_by = %q want backend", q.Get("group_by"))
	}
}

func TestCostReport_CLI_NoQueryWhenFlagsUnset(t *testing.T) {
	var rec recQuery
	srv := newCostReportFakeDaemon(t, &rec)

	var runErr error
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	rerr := withServerURL(t, srv, func() error {
		os.Stdout = writer
		c := newCostReportCmd()
		runErr = c.RunE(c, []string{})
		if closeErr := writer.Close(); closeErr != nil {
			t.Errorf("writer.Close: %v", closeErr)
		}
		os.Stdout = oldStdout
		return runErr
	})
	if rerr != nil {
		t.Fatalf("run: %v", rerr)
	}
	_, _ = io.ReadAll(reader)
	if runErr != nil {
		t.Fatalf("runErr: %v", runErr)
	}

	q := lastQuery(t, &rec)
	if len(q) != 0 {
		t.Errorf("query should be empty when no flags are set, got %v", q)
	}
}

// TestCostReport_CLI_TableOmitsUSDColumn pins the no-rates case: when no
// row carries usd_estimate the table drops the usd column entirely
// (usd_estimate is per-row "where present").
func TestCostReport_CLI_TableOmitsUSDColumn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"group_by": "automation",
			"rows": [{"key": "lane-fill", "sessions": 3, "tokens_in": 5000, "tokens_out": 200}],
			"retention_days": 30,
			"lifetime_fallback": true
		}`)
	}))
	t.Cleanup(srv.Close)

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	var runErr error
	rerr := withServerURL(t, srv, func() error {
		os.Stdout = writer
		c := newCostReportCmd()
		runErr = c.RunE(c, []string{})
		if closeErr := writer.Close(); closeErr != nil {
			t.Errorf("writer.Close: %v", closeErr)
		}
		os.Stdout = oldStdout
		return runErr
	})
	if rerr != nil {
		t.Fatalf("run: %v", rerr)
	}
	capBuf, _ := io.ReadAll(reader)
	out := string(capBuf)

	if strings.Contains(out, "usd") {
		t.Errorf("no usd_estimate in any row → table must not mention usd:\n%s", out)
	}
	if !strings.Contains(out, "lane-fill") || !strings.Contains(out, "5000") {
		t.Errorf("tokens row missing:\n%s", out)
	}
	if !strings.Contains(out, "retention: 30 days") || !strings.Contains(out, "lifetime fallback: yes") {
		t.Errorf("footer missing:\n%s", out)
	}
}

func TestCostReport_CLI_ErrWhenDaemonUnreachable(t *testing.T) {
	old := serverURL
	serverURL = "http://127.0.0.1:1" // nothing listens there
	defer func() { serverURL = old }()

	c := newCostReportCmd()
	if err := c.RunE(c, []string{}); err == nil {
		t.Error("expected an error when the daemon is unreachable")
	}
}
