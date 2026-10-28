// cli_usage_test.go — fakes the daemon with an httptest server per subcommand
// and checks that the CLI sends the right request and renders the same advice
// the daemon returns, without recomputing or rewording anything.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

type usageCall struct {
	method string
	uri    string
	body   map[string]any
}

// usageDaemon starts a fake daemon recording each request (method, full URI
// with query, JSON body), points serverURL at it, and returns the recorder.
func usageDaemon(t *testing.T) func() []usageCall {
	t.Helper()
	var mu sync.Mutex
	var calls []usageCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		mu.Lock()
		calls = append(calls, usageCall{method: r.Method, uri: r.URL.RequestURI(), body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return func() []usageCall {
		mu.Lock()
		defer mu.Unlock()
		out := make([]usageCall, len(calls))
		copy(out, calls)
		return out
	}
}

func usageRun(t *testing.T, c *cobra.Command) error {
	t.Helper()
	c.SilenceUsage = true
	c.SilenceErrors = true
	return c.Execute()
}

// usageFixture is one AdviceResult the fake daemon answers with: a percent
// reading against a reset two and a half days away, extra credit on, the
// recent burn ahead of the week average, a typed remaining-work figure with
// a queued count, and the run-until-used-up policy's headline and notes.
const usageFixture = `{
  "variant":"all",
  "policy":"use-all",
  "advice":{
    "read_at":"2026-10-23T04:08:00Z",
    "percent":72,
    "next_reset_at":"2026-10-25T12:00:00Z",
    "days_left":2.62,
    "extra_credit_points":25,
    "ceiling":125,
    "lanes":2,
    "days_since_reset":4.38,
    "pace_line":84.5,
    "headroom":12.5,
    "recent_burn_per_24h":4.1,
    "week_avg_burn_per_24h":3.5,
    "projected_end_recent":96.2,
    "projected_end_avg":90.1,
    "verdict":"within",
    "verdict_avg":"within",
    "remaining_work":{"in_use":"typed","points":20,"queue_tasks":3,"cost_per_task":6.7},
    "by_lanes":[],
    "series":[],
    "daily":[],
    "advice":{
      "policy":"use-all",
      "lanes_suggested":3,
      "headline":"Within budget at 2 lanes; the week runs to the reset",
      "notes":["The operator chooses the number of lanes; this is advice only."]
    }
  }
}`

// usageNormalize collapses the rendered form to single-spaced text so the
// assertions are not broken by the 80-column word wrap.
func usageNormalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestUsageCalcAdviceTextForm(t *testing.T) {
	var gotMethod, gotURI string
	prev := serverURL
	t.Cleanup(func() { serverURL = prev })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURI = r.URL.RequestURI()
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/usage/advice" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"wrong request"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(usageFixture))
	}))
	t.Cleanup(srv.Close)
	serverURL = srv.URL

	var out strings.Builder
	c := newUsageAdviceCmd()
	c.SetOut(&out)
	c.SetArgs(nil)
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotURI != "/api/usage/advice" {
		t.Fatalf("daemon got %s %s, want GET /api/usage/advice", gotMethod, gotURI)
	}
	text := usageNormalize(out.String())
	for _, want := range []string{
		"usage: 72% read Fri 04:08, reset Sun 12:00 (2.62 days left)",
		"extra credit +25 points, ceiling 125 points",
		"pace line 84.5 points, headroom 12.5 points, 4.38 days since reset",
		"burn 4.1 points per 24h recent, 3.5 points per 24h week average",
		"projected end 96.2% recent (within budget), 90.1% week average (within budget)",
		"verdict: within budget at the recent burn; within budget at the week average",
		"lanes 2 running",
		"policy Run until used up",
		"remaining work 20 points (typed); 3 queued tasks",
		"advice: Within budget at 2 lanes; the week runs to the reset",
		"The operator chooses the number of lanes; this is advice only.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "runs out: not before the reset") {
		t.Errorf("missing the run-out line in:\n%s", text)
	}
	// The CLI form ends with the advice's own notes, not a comm reply line.
	if strings.Contains(text, "Reply:") {
		t.Errorf("the CLI form must not carry the comm reply line:\n%s", text)
	}
}

func TestUsageCalcAdviceFlagsAsQuery(t *testing.T) {
	seen := usageDaemon(t)
	var out strings.Builder
	c := newUsageAdviceCmd()
	c.SetOut(&out)
	c.SetArgs([]string{"--variant", "pace-gauge", "--policy", "pace", "--lanes", "2"})
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	got := calls[0]
	if got.method != http.MethodGet {
		t.Errorf("method = %s, want GET", got.method)
	}
	want := "/api/usage/advice?lanes=2&policy=pace&variant=pace-gauge"
	if got.uri != want {
		t.Errorf("uri = %q, want %q", got.uri, want)
	}
}

func TestUsageCalcAdviceLocalValidation(t *testing.T) {
	seen := usageDaemon(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"bad variant", []string{"--variant", "bogus"}, "unknown variant"},
		{"bad policy", []string{"--policy", "bogus"}, "unknown policy"},
		{"negative lanes", []string{"--lanes", "-1"}, "lanes must be 0 or more"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newUsageAdviceCmd()
			c.SetArgs(tc.args)
			err := usageRun(t, c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if n := len(seen()); n != 0 {
				t.Errorf("daemon hit %d times, want 0 (local validation)", n)
			}
		})
	}
}

func TestUsageCalcAdviceJSON(t *testing.T) {
	prev := serverURL
	t.Cleanup(func() { serverURL = prev })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(usageFixture))
	}))
	t.Cleanup(srv.Close)
	serverURL = srv.URL

	var out strings.Builder
	c := newUsageAdviceCmd()
	c.SetOut(&out)
	c.SetArgs([]string{"--json"})
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	// The daemon's body, byte for byte: the fixture keeps its compact
	// "percent":72 shape, which a re-encode (json.MarshalIndent) would break.
	if want := usageFixture + "\n"; out.String() != want {
		t.Errorf("--json = %q, want the body as received:\n%s", out.String(), want)
	}
	if strings.Contains(out.String(), "advice: Within budget") {
		t.Errorf("--json must not print the text form:\n%s", out.String())
	}
}

func TestUsageCalcReadingAddPercent(t *testing.T) {
	seen := usageDaemon(t)
	c := newUsageReadingAddCmd()
	c.SetArgs([]string{"--percent", "72",
		"--at", "2026-10-24T09:30:00Z",
		"--session-percent", "60",
		"--lanes", "3"})
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	got := calls[0]
	if got.method != http.MethodPost || got.uri != "/api/usage/readings" {
		t.Fatalf("got %s %s, want POST /api/usage/readings", got.method, got.uri)
	}
	b := got.body
	if b["percent"] != float64(72) {
		t.Errorf("percent = %v, want 72", b["percent"])
	}
	if b["at"] != "2026-10-24T09:30:00Z" {
		t.Errorf("at = %v, want 2026-10-24T09:30:00Z", b["at"])
	}
	if b["session_percent"] != float64(60) {
		t.Errorf("session_percent = %v, want 60", b["session_percent"])
	}
	if b["lanes"] != float64(3) {
		t.Errorf("lanes = %v, want 3", b["lanes"])
	}
	if b["source"] != "typed" {
		t.Errorf("source = %v, want typed", b["source"])
	}
	if _, ok := b["spend_usd"]; ok {
		t.Errorf("percent reading must not carry a spend figure: %v", b)
	}
}

func TestUsageCalcReadingAddSpend(t *testing.T) {
	seen := usageDaemon(t)
	c := newUsageReadingAddCmd()
	c.SetArgs([]string{"--spend-usd", "12.5", "--session-percent", "45.2"})
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	b := calls[0].body
	if b["spend_usd"] != 12.5 {
		t.Errorf("spend_usd = %v, want 12.5", b["spend_usd"])
	}
	if b["session_percent"] != 45.2 {
		t.Errorf("session_percent = %v, want 45.2", b["session_percent"])
	}
	if b["source"] != "typed" {
		t.Errorf("source = %v, want typed", b["source"])
	}
	if _, ok := b["percent"]; ok {
		t.Errorf("spend reading must not carry a percent: %v", b)
	}
}

func TestUsageCalcReadingAddValidation(t *testing.T) {
	seen := usageDaemon(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"neither figure", []string{}, "carry --percent N or --spend-usd N"},
		{"both figures", []string{"--percent", "72", "--spend-usd", "5"}, "carry either --percent or --spend-usd, not both"},
		{"negative percent", []string{"--percent", "-1"}, "percent must be 0 or more"},
		{"percent beyond the ceiling", []string{"--percent", "2000"}, "percent must be from 0 to"},
		{"negative spend", []string{"--spend-usd", "-5"}, "spend must be 0 or more"},
		{"bad at", []string{"--percent", "72", "--at", "Thursday"}, "RFC3339"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newUsageReadingAddCmd()
			c.SetArgs(tc.args)
			err := usageRun(t, c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if n := len(seen()); n != 0 {
				t.Errorf("daemon hit %d times, want 0 (local validation)", n)
			}
		})
	}
}

func TestUsageCalcReadingList(t *testing.T) {
	seen := usageDaemon(t)
	c := newUsageReadingListCmd()
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].method != http.MethodGet || calls[0].uri != "/api/usage/readings" {
		t.Errorf("got %s %s, want GET /api/usage/readings", calls[0].method, calls[0].uri)
	}
}

func TestUsageCalcReadingDelete(t *testing.T) {
	seen := usageDaemon(t)
	c := newUsageReadingDeleteCmd()
	c.SetArgs([]string{"deadbeefcafe0000"})
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	want := "/api/usage/readings?id=deadbeefcafe0000"
	if calls[0].method != http.MethodDelete || calls[0].uri != want {
		t.Errorf("got %s %s, want DELETE %s", calls[0].method, calls[0].uri, want)
	}
}

func TestUsageCalcExtraCredit(t *testing.T) {
	seen := usageDaemon(t)
	c := newUsageExtraCreditCmd()
	c.SetArgs([]string{"25"})
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	calls := seen()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].method != http.MethodPut || calls[0].uri != "/api/usage/extra-credit" {
		t.Errorf("got %s %s, want PUT /api/usage/extra-credit", calls[0].method, calls[0].uri)
	}
	if calls[0].body["points"] != float64(25) {
		t.Errorf("points = %v, want 25", calls[0].body["points"])
	}
}

func TestUsageCalcExtraCreditValidation(t *testing.T) {
	seen := usageDaemon(t)
	for _, bad := range []string{"-3", "a lot"} {
		c := newUsageExtraCreditCmd()
		c.SetArgs([]string{"--", bad}) // -- so cobra does not parse a negative number as a flag
		err := usageRun(t, c)
		if err == nil || !strings.Contains(err.Error(), "0 or more points") {
			t.Fatalf("arg %q: err = %v, want 0 or more points", bad, err)
		}
	}
	if n := len(seen()); n != 0 {
		t.Errorf("daemon hit %d times, want 0 (local validation)", n)
	}
}

func TestUsageCalcRemainingWork(t *testing.T) {
	t.Run("set a number of points", func(t *testing.T) {
		seen := usageDaemon(t)
		c := newUsageRemainingWorkCmd()
		c.SetArgs([]string{"15.5"})
		if err := usageRun(t, c); err != nil {
			t.Fatal(err)
		}
		calls := seen()
		if len(calls) != 1 {
			t.Fatalf("calls = %d, want 1", len(calls))
		}
		if calls[0].method != http.MethodPut || calls[0].uri != "/api/usage/remaining-work" {
			t.Errorf("got %s %s, want PUT /api/usage/remaining-work", calls[0].method, calls[0].uri)
		}
		if calls[0].body["points"] != 15.5 {
			t.Errorf("points = %v, want 15.5", calls[0].body["points"])
		}
	})

	t.Run("clear removes the figure", func(t *testing.T) {
		seen := usageDaemon(t)
		c := newUsageRemainingWorkCmd()
		c.SetArgs([]string{"clear"})
		if err := usageRun(t, c); err != nil {
			t.Fatal(err)
		}
		calls := seen()
		if len(calls) != 1 {
			t.Fatalf("calls = %d, want 1", len(calls))
		}
		if calls[0].method != http.MethodDelete || calls[0].uri != "/api/usage/remaining-work" {
			t.Errorf("got %s %s, want DELETE /api/usage/remaining-work", calls[0].method, calls[0].uri)
		}
	})
}

func TestUsageCalcRemainingWorkValidation(t *testing.T) {
	seen := usageDaemon(t)
	c := newUsageRemainingWorkCmd()
	c.SetArgs([]string{"--", "-2"}) // -- so cobra does not parse a negative number as a flag
	err := usageRun(t, c)
	if err == nil || !strings.Contains(err.Error(), "0 or more points") {
		t.Fatalf("err = %v, want 0 or more points", err)
	}
	if n := len(seen()); n != 0 {
		t.Errorf("daemon hit %d times, want 0 (local validation)", n)
	}
}

// TestUsageCalcAdviceNoOverrideSendsNoQuery pins the config passthrough: with
// no --variant / --policy / --lanes the CLI must not override the daemon's
// stored settings — the request goes out to the plain path and the stored
// variant and policy come back in the body without the CLI touching them.
func TestUsageCalcAdviceNoOverrideSendsNoQuery(t *testing.T) {
	var gotURI string
	prev := serverURL
	t.Cleanup(func() { serverURL = prev })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"variant":"burn-ledger","policy":"finish-early","advice":{}}`))
	}))
	t.Cleanup(srv.Close)
	serverURL = srv.URL

	var out strings.Builder
	c := newUsageAdviceCmd()
	c.SetOut(&out)
	if err := usageRun(t, c); err != nil {
		t.Fatal(err)
	}
	if gotURI != "/api/usage/advice" {
		t.Errorf("uri = %q, want /api/usage/advice with no query (stored settings must pass through)", gotURI)
	}
	if !strings.Contains(out.String(), "no weekly readings yet") {
		t.Errorf("expected the no-reading empty state, got:\n%s", out.String())
	}
}

// TestUsageCalcConfigSetVariantPassthrough pins `config set
// usage.calculator.variant <v>` — the calculator's settings go through the
// existing config command unchanged: it PUTs /api/config with that key and
// value, with no usage-specific rewiring of the key.
func TestUsageCalcConfigSetVariantPassthrough(t *testing.T) {
	type call struct{ method, uri, raw string }
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, call{r.Method, r.URL.RequestURI(), string(raw)})
		mu.Unlock()
		// The daemon parses the body as JSON; the first (unquoted) attempt is
		// rejected, the quoted one is accepted.
		if !json.Valid(raw) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid json"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	prevURL := serverURL
	t.Cleanup(func() { serverURL = prevURL })
	serverURL = srv.URL

	// Keep the command from reading the operator's real config off disk: a
	// path that does not exist loads the defaults.
	prevCfg := cfgPath
	t.Cleanup(func() { cfgPath = prevCfg })
	cfgPath = filepath.Join(t.TempDir(), "config.yaml")

	c := newConfigSetCmd()
	c.SetArgs([]string{"usage.calculator.variant", "lane-planner"})
	if err := usageRun(t, c); err != nil {
		t.Fatalf("config set: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("the daemon was never called")
	}
	last := calls[len(calls)-1]
	if last.method != http.MethodPut || last.uri != "/api/config" {
		t.Errorf("got %s %s, want PUT /api/config", last.method, last.uri)
	}
	// The key passes through unchanged and the accepted body carries the exact
	// value.
	if !strings.Contains(last.raw, `"usage.calculator.variant"`) {
		t.Errorf("accepted body lost the key: %s", last.raw)
	}
	if !strings.Contains(last.raw, `"lane-planner"`) {
		t.Errorf("accepted body = %q, want the value lane-planner", last.raw)
	}
}
