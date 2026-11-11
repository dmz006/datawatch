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

// runCostReportCLI runs `cost report` against srv and returns the stdout
// text (pipe capture, same pattern as the table tests above).
func runCostReportCLI(t *testing.T, srv *httptest.Server) string {
	t.Helper()
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
	if runErr != nil {
		t.Fatalf("runErr: %v", runErr)
	}
	out, _ := io.ReadAll(reader)
	return string(out)
}

// TestCostReport_CLI_BillingColumns pins the billing surface: a
// bundled-plan row renders the API-equivalent estimate (money), the usage
// points it represents, its share of the monthly price, and the
// "API-equivalent, not billed" label; the unprofiled row keeps its
// assumed label. No usd column appears (bundled rows are not billed).
func TestCostReport_CLI_BillingColumns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"group_by": "backend",
			"rows": [
				{"key": "claude-code", "sessions": 2, "tokens_in": 400000, "tokens_out": 30000,
				 "api_equivalent_usd": 12.5, "estimated_usage_points": 25, "share_of_monthly_price": 0.05,
				 "billing_mode": "bundled_plan", "billing_label": "bundled plan — API-equivalent, not billed"},
				{"key": "ollama", "sessions": 1, "tokens_in": 1000, "tokens_out": 100,
				 "billing_mode": "api_metered", "billing_label": "api_metered (assumed — no usage.billing entry)"}
			],
			"retention_days": 31,
			"retention_note": "spend figures cover up to 31 days back",
			"lifetime_fallback": false
		}`)
	}))
	t.Cleanup(srv.Close)

	out := runCostReportCLI(t, srv)
	for _, frag := range []string{
		"claude-code", "ollama",
		"api_equiv_usd", "usage_pts", "share_of_price", "billing", // headers
		"$12.50", "25", "5.0%", // the bundled row's estimate, points, share
		"API-equivalent, not billed",             // the label, verbatim
		"assumed",                                // the unprofiled row's label
		"spend figures cover up to 31 days back", // the retention note in the footer
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("output misses %q:\n%s", frag, out)
		}
	}
	// A bare "usd" column must not appear: bundled rows carry an
	// API-equivalent estimate, not a bill. ("api_equiv_usd" contains the
	// substring, so compare whole cells.)
	headerCells := strings.Split(strings.SplitN(out, "\n", 2)[0], "\t")
	for _, cell := range headerCells {
		if strings.TrimSpace(cell) == "usd" {
			t.Errorf("bare usd column must not render next to api_equiv_usd:\n%s", out)
			break
		}
	}
}

// TestCostReport_CLI_ReleaseUnits pins group_by release: shipped-unit
// counts render in the units column and unshipped work lands under the
// "unreleased" key.
func TestCostReport_CLI_ReleaseUnits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"group_by": "release",
			"rows": [
				{"key": "v1.2.0", "sessions": 3, "tokens_in": 90000, "tokens_out": 4000, "units": 3},
				{"key": "unreleased", "sessions": 2, "tokens_in": 60000, "tokens_out": 2000}
			],
			"retention_days": 31,
			"retention_note": "up to 31 days back",
			"lifetime_fallback": false
		}`)
	}))
	t.Cleanup(srv.Close)

	out := runCostReportCLI(t, srv)
	for _, frag := range []string{"units", "v1.2.0", "unreleased", "3"} {
		if !strings.Contains(out, frag) {
			t.Errorf("output misses %q:\n%s", frag, out)
		}
	}
}

func TestCostReport_ExtendedColumnsAndAttribution(t *testing.T) {
	body := `{"group_by":"automaton","rows":[{"key":"a1","sessions":3,"tokens_in":10,"tokens_out":5,
"cache_read":7,"cache_creation":2,"tool_calls":9,"span_seconds":3600,"active_seconds":90,
"api_equivalent_usd":2.5,"window_note":"started mid-window"}],
"window_note":"older than ledger","retention_days":31,
"attribution":{"automaton_launched":4,"unattributed":1,"ok":false,"note":"check s9"}}`
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := printCostReport([]byte(body))
	_ = w.Close()
	os.Stdout = oldStdout
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	out := string(b)
	for _, frag := range []string{"cache_read", "tool_calls", "1h0m0s", "1m30s", "API-equivalent estimate, not a bill",
		"window note (a1): started mid-window", "window note: older than ledger",
		"attribution check: UNATTRIBUTED — 1 of 4"} {
		if !strings.Contains(out, frag) {
			t.Errorf("missing %q:\n%s", frag, out)
		}
	}
}

func TestAutomatonCost_DetailLines(t *testing.T) {
	body := []byte(`{"id":"x","cost":{"key":"x","sessions":2,"tokens_in":100,"tokens_out":50,"cache_read":30,"cache_creation":4,
"tool_calls":12,"span_seconds":120,"active_seconds":60,"api_equivalent_usd":1.5,"window_note":"partial","report_window_note":"ledger 31d"}}`)
	got := strings.Join(automatonCostLines(body), "\n")
	for _, frag := range []string{"2 sessions", "100/50/30+4", "12 tool calls", "span 2m0s", "active 1m0s",
		"$1.50 (API-equivalent, not a bill)", "cost window note: partial", "cost window note: ledger 31d"} {
		if !strings.Contains(got, frag) {
			t.Errorf("missing %q:\n%s", frag, got)
		}
	}
	if automatonCostLines([]byte(`{"id":"x"}`)) != nil || automatonCostLines([]byte(`nope`)) != nil {
		t.Error("expected nil without cost")
	}
}
