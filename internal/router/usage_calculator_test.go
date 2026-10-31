package router_test

// Tests for the comm-channel usage calculator (`usage …`) — the comm-channel
// section of docs/plans/2026-10-23-usage-calculator-design.md, mirrored on the
// budget handler: parse forms, the loopback requests, the 50-column reply
// built with usagecalc.FormatComm, and the configure path through the router.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// usageCalcAdviceJSON is the fixed advice the stand-in answers with: one
// reading in, a verdict, a typed remaining-work figure, a session and a
// policy headline with notes — enough to exercise the shared text layer's
// field order and the conditional lines.
const usageCalcAdviceJSON = `{
  "variant": "burn-ledger",
  "policy": "pace",
  "stale": false,
  "advice": {
    "read_at": "2026-09-09T16:10:00Z",
    "percent": 73.4,
    "next_reset_at": "2026-09-13T19:00:00Z",
    "extra_credit_points": 0,
    "ceiling": 100,
    "lanes": 1,
    "days_since_reset": 2,
    "days_left": 3.5,
    "pace_line": 57.1,
    "headroom": 16.3,
    "recent_burn_per_24h": 3.2,
    "week_avg_burn_per_24h": 2.8,
    "projected_end_recent": 97.2,
    "projected_end_avg": 95.8,
    "run_out_at_recent": "2026-09-12T18:00:00Z",
    "hours_early_recent": 25,
    "verdict": "within",
    "verdict_avg": "within",
    "headroom_note": "",
    "remaining_work": {"in_use": "typed", "points": 15.5, "queue_tasks": 2, "cost_per_task": 4},
    "session": {"percent": 41, "read_at": "2026-09-09T16:10:00Z", "label": "session"},
    "advice": {
      "policy": "pace",
      "lanes_suggested": 2,
      "headline": "One lane at about 3.2 points per 24h finishes well before the reset.",
      "notes": ["Keep the current pace; the allowance runs out Mon 18:00."]
    },
    "series": [],
    "daily": []
  }
}`

// usageCalcReq is one request the stand-in answered.
type usageCalcReq struct {
	method string
	path   string
	query  url.Values
	body   string
}

// usageCalcStandInAdvice returns a router whose REST loopback is an httptest
// stand-in: it records every request and answers GET /api/usage/advice with
// the fixed advice, every other call with a 204.
func usageCalcStandInAdvice(t *testing.T) (*router.Router, *[]usageCalcReq) {
	t.Helper()
	var recs []usageCalcReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recs = append(recs, usageCalcReq{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
			body:   string(body),
		})
		if r.Method == http.MethodGet && r.URL.Path == "/api/usage/advice" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(usageCalcAdviceJSON))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndexByte(srv.URL, ':')+1:])
	if err != nil {
		t.Fatal(err)
	}
	r := router.NewRouter("calc", "g1", nil, nil, 10, nil)
	r.SetWebPort(port)
	return r, &recs
}

func TestUsageCalcParseAdvice(t *testing.T) {
	t.Run("bare usage is the advice with no overrides", func(t *testing.T) {
		c := router.Parse("usage")
		if c.Type != router.CmdUsage || c.UsageVerb != "" {
			t.Fatalf("got type=%s verb=%q, want usage/\"\"", c.Type, c.UsageVerb)
		}
	})

	t.Run("advice forwards variant, policy and lanes", func(t *testing.T) {
		c := router.Parse("usage advice variant=burn-ledger policy=pace lanes=3")
		if c.Type != router.CmdUsage {
			t.Fatalf("type = %s", c.Type)
		}
		if c.UsageVerb != "advice" || c.UsageVariant != "burn-ledger" || c.UsagePolicy != "pace" || c.UsageLanes != 3 {
			t.Fatalf("got verb=%q variant=%q policy=%q lanes=%d", c.UsageVerb, c.UsageVariant, c.UsagePolicy, c.UsageLanes)
		}
	})

	t.Run("verbs are case-insensitive, keys fold, values pass through in any order", func(t *testing.T) {
		c := router.Parse("USAGE Advice Lanes=2 Variant=Lane-Planner")
		if c.Type != router.CmdUsage || c.UsageVerb != "advice" {
			t.Fatalf("got type=%s verb=%q", c.Type, c.UsageVerb)
		}
		if c.UsageVariant != "Lane-Planner" || c.UsageLanes != 2 || c.UsagePolicy != "" {
			t.Fatalf("got variant=%q lanes=%d policy=%q", c.UsageVariant, c.UsageLanes, c.UsagePolicy)
		}
	})
}

func TestUsageCalcParseReading(t *testing.T) {
	t.Run("percent with an ignored lanes and an optional session", func(t *testing.T) {
		c := router.Parse("usage reading 72.5 lanes=2 session=41")
		if c.Type != router.CmdUsage || c.UsageVerb != "reading" {
			t.Fatalf("got type=%s verb=%q", c.Type, c.UsageVerb)
		}
		if c.UsagePercent != 72.5 || c.UsageLanes != 0 || c.UsageLanesSet {
			t.Fatalf("got percent=%v lanes=%d set=%v, want lanes ignored", c.UsagePercent, c.UsageLanes, c.UsageLanesSet)
		}
		if c.UsageSession == nil || *c.UsageSession != 41 {
			t.Fatalf("session = %v, want 41", c.UsageSession)
		}
	})

	t.Run("a whole percent and no optional keys", func(t *testing.T) {
		c := router.Parse("usage reading 55")
		if c.UsageVerb != "reading" || c.UsagePercent != 55 || c.UsageLanes != 0 || c.UsageSession != nil {
			t.Fatalf("got verb=%q percent=%v lanes=%d session=%v", c.UsageVerb, c.UsagePercent, c.UsageLanes, c.UsageSession)
		}
	})

	t.Run("a session without a percent is still a reading", func(t *testing.T) {
		c := router.Parse("usage reading 50 session=88")
		if c.UsageVerb != "reading" || c.UsagePercent != 50 {
			t.Fatalf("got verb=%q percent=%v", c.UsageVerb, c.UsagePercent)
		}
		if c.UsageSession == nil || *c.UsageSession != 88 {
			t.Fatalf("session = %v, want 88", c.UsageSession)
		}
	})
}

func TestUsageCalcParseExtraWork(t *testing.T) {
	t.Run("extra points", func(t *testing.T) {
		c := router.Parse("usage extra 20.25")
		if c.Type != router.CmdUsage || c.UsageVerb != "extra" || c.UsagePoints != 20.25 || c.UsageClear {
			t.Fatalf("got verb=%q points=%v clear=%v", c.UsageVerb, c.UsagePoints, c.UsageClear)
		}
	})

	t.Run("work points", func(t *testing.T) {
		c := router.Parse("usage work 15.5")
		if c.UsageVerb != "work" || c.UsagePoints != 15.5 || c.UsageClear {
			t.Fatalf("got verb=%q points=%v clear=%v", c.UsageVerb, c.UsagePoints, c.UsageClear)
		}
	})

	t.Run("work clear", func(t *testing.T) {
		c := router.Parse("usage work clear")
		if c.UsageVerb != "work" || !c.UsageClear || c.UsagePoints != 0 {
			t.Fatalf("got verb=%q clear=%v points=%v", c.UsageVerb, c.UsageClear, c.UsagePoints)
		}
		if cc := router.Parse("usage work CLEAR"); cc.UsageVerb != "work" || !cc.UsageClear {
			t.Fatalf("CLEAR form: got verb=%q clear=%v", cc.UsageVerb, cc.UsageClear)
		}
	})
}

func TestUsageCalcParseInvalid(t *testing.T) {
	badForms := []string{
		"usage help",
		"usage advice lanes=x",
		"usage advice lanes=-1",
		"usage advice variant=",
		"usage advice variant=a variant=b",
		"usage advice variant=a VARIANT=b", // same key twice, any case
		"usage advice bogus=1",
		"usage advice session=3", // session belongs to the reading form
		"usage reading",
		"usage reading x",
		"usage reading -5",
		"usage reading 5 session", // no value
		"usage reading 5 session=-1",
		"usage reading 5 extra=3",
		"usage extra",
		"usage extra -1",
		"usage extra 2 3",
		"usage extra clear", // only work clears
		"usage work",
		"usage work x",
		"usage work clear extra",
	}
	for _, in := range badForms {
		c := router.Parse(in)
		if c.Type != router.CmdUsage {
			t.Errorf("%q: type = %s, want usage", in, c.Type)
			continue
		}
		if c.UsageVerb != "invalid" {
			t.Errorf("%q: verb = %q, want invalid", in, c.UsageVerb)
		}
	}
}

func TestUsageCalcParseNotUsage(t *testing.T) {
	// "usages" and other lookalikes must not bind to the new command.
	for _, in := range []string{"usages", "usage_x", "usage_backoff status", "usage-backoff all"} {
		c := router.Parse(in)
		if c.Type == router.CmdUsage {
			t.Errorf("%q bound to the usage calculator command", in)
		}
	}
	// usage_backoff still parses to its own command.
	if c := router.Parse("usage_backoff status"); c.Type != router.CmdUsageBackoff {
		t.Fatalf("usage_backoff = %s, want usage_backoff", c.Type)
	}
}

func TestUsageCalcAdviceHandler(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if len(*recs) != 1 {
		t.Fatalf("requests = %v, want exactly one", *recs)
	}
	req := (*recs)[0]
	if req.method != "GET" || req.path != "/api/usage/advice" {
		t.Fatalf("request = %s %s", req.method, req.path)
	}
	if len(req.query) != 0 {
		t.Fatalf("query = %v, want none", req.query)
	}
	assertUsageReply(t, out[0])
}

func TestUsageCalcAdviceOverrides(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage advice variant=burn-ledger policy=pace-with-safety lanes=3")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if len(*recs) != 1 {
		t.Fatalf("requests = %v, want exactly one", *recs)
	}
	req := (*recs)[0]
	want := map[string]string{"variant": "burn-ledger", "policy": "pace-with-safety", "lanes": "3"}
	if req.method != "GET" || req.path != "/api/usage/advice" {
		t.Fatalf("request = %s %s", req.method, req.path)
	}
	for k, v := range want {
		if req.query.Get(k) != v {
			t.Errorf("forwarded %s = %q, want %q", k, req.query.Get(k), v)
		}
	}
	if extra := len(req.query); extra != len(want) {
		t.Errorf("query has %d keys, want %d: %v", extra, len(want), req.query)
	}
	assertUsageReply(t, out[0])
}

func TestUsageCalcReadingHandler(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	// lanes=2 still parses for older callers, but never reaches the daemon.
	out := r.HandleTestMessage("usage reading 72.5 lanes=2 session=41")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if len(*recs) != 2 {
		t.Fatalf("requests = %v, want the write and the refreshed advice", *recs)
	}
	w := (*recs)[0]
	if w.method != "POST" || w.path != "/api/usage/readings" {
		t.Fatalf("first request = %s %s, want POST /api/usage/readings", w.method, w.path)
	}
	for _, sub := range []string{`"percent":72.5`, `"session_percent":41`, `"source":"typed"`} {
		if !strings.Contains(w.body, sub) {
			t.Errorf("body does not contain %s: %s", sub, w.body)
		}
	}
	if strings.Contains(w.body, "lanes") {
		t.Errorf("lanes reached the request body: %s", w.body)
	}
	if w.query.Has("lanes") {
		t.Errorf("lanes reached the query: %v", w.query)
	}
	if (*recs)[1].query.Get("lanes") != "" {
		t.Errorf("the refreshed advice carried lanes: %v", (*recs)[1].query)
	}
	if (*recs)[1].method != "GET" || (*recs)[1].path != "/api/usage/advice" {
		t.Fatalf("second request = %s %s, want the refreshed advice", (*recs)[1].method, (*recs)[1].path)
	}
	if len(out) != 1 || !strings.Contains(out[0], "reading stored: 72.5%") {
		t.Fatalf("reply = %q, wants the stored reading acknowledged", out)
	}
	assertUsageReply(t, out[0])
}

// An older caller's lanes=N on a reading parses (it is not an invalid form)
// and is dropped, whatever its value.
func TestUsageCalcReadingLanesParsedNotSent(t *testing.T) {
	for _, form := range []string{"usage reading 40 lanes=1", "usage reading 40 lanes=3 session=10", "usage reading 40 session=10 lanes=2", "usage reading 40 lanes=0", "usage reading 40 lanes=-1", "usage reading 40 lanes=x"} {
		r, recs := usageCalcStandInAdvice(t)
		out := r.HandleTestMessage(form)
		if len(out) == 0 || !strings.Contains(out[0], "reading stored: 40%") {
			t.Fatalf("%q: reply = %q, want the reading stored", form, out)
		}
		if len(*recs) == 0 || (*recs)[0].method != "POST" {
			t.Fatalf("%q: requests = %v", form, *recs)
		}
		if strings.Contains((*recs)[0].body, "lanes") {
			t.Errorf("%q: lanes reached the request body: %s", form, (*recs)[0].body)
		}
	}
}

// usage advice lanes=N is the what-if count and is sent, including 0.
func TestUsageCalcAdviceLanesWhatIfIncludingZero(t *testing.T) {
	for _, tc := range []struct{ form, want string }{
		{"usage advice lanes=0", "0"},
		{"usage advice lanes=3", "3"},
	} {
		r, recs := usageCalcStandInAdvice(t)
		if out := r.HandleTestMessage(tc.form); len(out) == 0 {
			t.Fatalf("%q: no reply", tc.form)
		}
		if len(*recs) != 1 {
			t.Fatalf("%q: requests = %v, want one", tc.form, *recs)
		}
		if got := (*recs)[0].query; !got.Has("lanes") || got.Get("lanes") != tc.want {
			t.Errorf("%q: query = %v, want lanes=%s", tc.form, got, tc.want)
		}
	}
	// No lanes given: nothing is sent.
	r, recs := usageCalcStandInAdvice(t)
	r.HandleTestMessage("usage advice")
	if len(*recs) != 1 || (*recs)[0].query.Has("lanes") {
		t.Errorf("bare advice sent lanes: %v", *recs)
	}
}

func TestUsageCalcReadingBare(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage reading 55")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	w := (*recs)[0]
	for _, sub := range []string{`"percent":55`, `"source":"typed"`} {
		if !strings.Contains(w.body, sub) {
			t.Errorf("body does not contain %s: %s", sub, w.body)
		}
	}
	for _, absent := range []string{`"lanes"`, `"session_percent"`} {
		if strings.Contains(w.body, absent) {
			t.Errorf("body carries %s though the form had no such key: %s", absent, w.body)
		}
	}
}

func TestUsageCalcExtraHandler(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage extra 20.25")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if len(*recs) != 2 {
		t.Fatalf("requests = %v", *recs)
	}
	w := (*recs)[0]
	if w.method != "PUT" || w.path != "/api/usage/extra-credit" {
		t.Fatalf("first request = %s %s, want PUT /api/usage/extra-credit", w.method, w.path)
	}
	if !strings.Contains(w.body, `"points":20.25`) {
		t.Errorf("body = %s, wants the points figure", w.body)
	}
	if len(out) != 1 || !strings.Contains(out[0], "extra credit set to 20.25 points") {
		t.Fatalf("reply = %q, wants the extra credit acknowledged", out)
	}
	assertUsageReply(t, out[0])
}

func TestUsageCalcWorkHandler(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage work 15.5")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if len(*recs) != 2 {
		t.Fatalf("requests = %v", *recs)
	}
	w := (*recs)[0]
	if w.method != "PUT" || w.path != "/api/usage/remaining-work" {
		t.Fatalf("first request = %s %s, want PUT /api/usage/remaining-work", w.method, w.path)
	}
	if !strings.Contains(w.body, `"points":15.5`) {
		t.Errorf("body = %s, wants the points figure", w.body)
	}
	if len(out) != 1 || !strings.Contains(out[0], "remaining work set to 15.5 points") {
		t.Fatalf("reply = %q, wants the remaining work acknowledged", out)
	}
}

func TestUsageCalcWorkClear(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage work clear")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if len(*recs) != 2 {
		t.Fatalf("requests = %v", *recs)
	}
	w := (*recs)[0]
	if w.method != "DELETE" || w.path != "/api/usage/remaining-work" {
		t.Fatalf("first request = %s %s, want DELETE /api/usage/remaining-work", w.method, w.path)
	}
	if w.body != "" {
		t.Errorf("clear carries a body: %q", w.body)
	}
	if len(out) != 1 || !strings.Contains(out[0], "remaining work cleared") {
		t.Fatalf("reply = %q, wants the clear acknowledged", out)
	}
}

func TestUsageCalcInvalidFormNoRequest(t *testing.T) {
	r, recs := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage nope 2")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if len(*recs) != 0 {
		t.Fatalf("an invalid form reached the daemon: %v", *recs)
	}
	for _, form := range []string{
		"usage: usage",
		"usage: usage advice [variant=V] [policy=P] [lanes=N]",
		"usage: usage reading <percent> [session=N]",
		"usage: usage extra <points>",
		"usage: usage work <points|clear>",
		// lanes is a what-if on the advice only.
		"lanes=N on advice is a what-if, not saved.",
		"A reading is stamped with the running lanes.",
	} {
		if !strings.Contains(out[0], form) {
			t.Errorf("reply lacks the form %q:\n%s", form, out[0])
		}
	}
}

func TestUsageCalcReplyWidth(t *testing.T) {
	r, _ := usageCalcStandInAdvice(t)

	out := r.HandleTestMessage("usage")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	text := out[0]
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if len(line) > usagecalc.CommWidth {
			t.Errorf("reply line %d is %d columns, want at most %d: %q", i, len(line), usagecalc.CommWidth, line)
		}
	}

	// The comm form's field order survives the round trip: the reading line
	// first, the verdict before the lanes and policy lines, the advice
	// headline before its note, the session line secondary.
	order := []string{
		"usage: 73.4% read",
		"pace line 57.1 points",
		"burn 3.2 points per 24h recent",
		"within budget",
		"lanes 1 running",
		"policy Pace",
		"remaining work 15.5 points (typed); 2 queued tasks",
		"advice: One lane at about 3.2 points per 24h",
		"allowance runs",
		"session 41% (secondary, no verdict)",
	}
	var last int
	for _, sub := range order {
		p := strings.Index(text, sub)
		if p < 0 {
			t.Errorf("reply lacks %q\n%s", sub, text)
			continue
		}
		if p < last {
			t.Errorf("%q appears out of the design doc's field order (after position %d, at %d)", sub, last, p)
		}
		last = p
	}
}

func assertUsageReply(t *testing.T, got string) {
	t.Helper()
	for _, sub := range []string{
		// The shared text layer's field order, condensed (wrap-safe substrings).
		"usage: 73.4% read",
		"within budget",
		"advice: One lane at about 3.2 points per 24h",
		"session 41% (secondary, no verdict)",
		// The shared text layer's own reply line.
		"Reply: usage advice lanes=2",
		// The grammar's reply hint closing the reply, one form per line.
		"reply with one of:",
		"usage advice [variant=V] [policy=P] [lanes=N]",
		"usage reading <pct> [session=N]",
		"usage extra <pts>",
		"usage work <pts|clear>",
		"lanes=N is a what-if, not saved.",
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("reply lacks %q\nfull reply:\n%s", sub, got)
		}
	}
}

func TestUsageCalcConfigureThroughRouter(t *testing.T) {
	r, _ := usageCalcStandInAdvice(t)

	var gotKey, gotVal string
	var ok bool
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotVal, ok = key, value, true
		return nil
	})

	out := r.HandleTestMessage("configure usage.calculator.variant=burn-ledger")
	if len(out) == 0 {
		t.Fatal("no reply")
	}
	if !ok {
		t.Fatalf("configure did not reach the config sink; reply: %v", out)
	}
	if gotKey != "usage.calculator.variant" || gotVal != "burn-ledger" {
		t.Fatalf("got %s=%s, want usage.calculator.variant=burn-ledger", gotKey, gotVal)
	}
	if !strings.Contains(out[0], "usage.calculator.variant") {
		t.Fatalf("reply = %q, wants the key acknowledged", out[0])
	}
}
