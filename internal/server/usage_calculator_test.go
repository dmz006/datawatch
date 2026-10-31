// TestUsageCalc*: the calculator advice and remaining-work endpoints —
// the happy paths, the per-call overrides of one advice (variant, policy,
// lanes, cost), the error paths of each route, and the federation gate on
// every one of them, each route asking for its own capability (the
// capability constants live in the calculator's shared REST file).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/usagecalc"
)

// newUsageCalcServer wires the advice service onto a fresh keeper test
// server; no queue, so the finish-early estimate stays out of the numbers.
func newUsageCalcServer(t *testing.T) *Server {
	t.Helper()
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	if err := s.AttachUsageCalculator(nil); err != nil {
		t.Fatalf("AttachUsageCalculator: %v", err)
	}
	return s
}

func TestUsageCalcAdviceHappyPath(t *testing.T) {
	s := newUsageCalcServer(t)

	// One percent reading: the advice is built from it.
	if rr := doUsage(t, s, http.MethodPost, "/api/usage/readings",
		jsonMarshalUsage(t, usagecalc.Reading{At: time.Now().UTC().Add(-time.Hour), Percent: fptrUsage(61.5), Source: usagecalc.SourceTyped}), s.handleUsageReadings); rr.Code != http.StatusOK {
		t.Fatalf("POST reading = %d: %s", rr.Code, rr.Body.String())
	}

	rr := doUsage(t, s, http.MethodGet, "/api/usage/advice", "", s.handleUsageAdvice)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET advice = %d: %s", rr.Code, rr.Body.String())
	}
	var res usagecalc.AdviceResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode advice: %v", err)
	}
	// No settings stored, no override: the full form, and the newest
	// percent reading is the one the numbers read.
	if res.Variant != usagecalc.VariantAll {
		t.Errorf("variant = %q, want %q", res.Variant, usagecalc.VariantAll)
	}
	if res.Advice.Percent != 61.5 {
		t.Errorf("advice.percent = %v, want 61.5 (the newest percent reading)", res.Advice.Percent)
	}
}

func TestUsageCalcAdvicePerCallOverrides(t *testing.T) {
	s := newUsageCalcServer(t)

	rr := doUsage(t, s, http.MethodGet, "/api/usage/advice?variant=pace-gauge", "", s.handleUsageAdvice)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET variant=pace-gauge = %d: %s", rr.Code, rr.Body.String())
	}
	var res usagecalc.AdviceResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Variant != usagecalc.VariantPaceGauge {
		t.Errorf("variant = %q, want %q (the per-call override)", res.Variant, usagecalc.VariantPaceGauge)
	}

	// The lanes and cost overrides just need to be accepted; they reshape
	// the planning, not the endpoint's contract.
	for _, q := range []string{"lanes=2", "cost=4", "lanes=0&cost=0"} {
		if rr := doUsage(t, s, http.MethodGet, "/api/usage/advice?"+q, "", s.handleUsageAdvice); rr.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200: %s", q, rr.Code, rr.Body.String())
		}
	}
}

func TestUsageCalcAdviceErrorPaths(t *testing.T) {
	s := newUsageCalcServer(t)

	cases := []struct {
		name string
		req  string
		want int
	}{
		{"unknown variant", "/api/usage/advice?variant=bogus", http.StatusBadRequest},
		{"negative lanes", "/api/usage/advice?lanes=-1", http.StatusBadRequest},
		{"non-numeric lanes", "/api/usage/advice?lanes=abc", http.StatusBadRequest},
		{"negative cost", "/api/usage/advice?cost=-3", http.StatusBadRequest},
	}
	for _, c := range cases {
		if rr := doUsage(t, s, http.MethodGet, c.req, "", s.handleUsageAdvice); rr.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, rr.Code, c.want, rr.Body.String())
		}
	}
}

// TestUsageCalcAdviceServerZone pins the server-zone markers end to end:
// the zone unset, the host zone faked via TZ, so the served advice reports
// the effective zone with server_zone true both in the numbers and in both
// text forms, and the config endpoint marks the same answer as server-sourced.
func TestUsageCalcAdviceServerZone(t *testing.T) {
	t.Setenv("TZ", "America/New_York")
	s := newUsageCalcServer(t)
	if rr := doUsage(t, s, http.MethodPost, "/api/usage/readings",
		jsonMarshalUsage(t, usagecalc.Reading{At: time.Now().UTC().Add(-time.Hour), Percent: fptrUsage(61.5), Source: usagecalc.SourceTyped}), s.handleUsageReadings); rr.Code != http.StatusOK {
		t.Fatalf("POST reading = %d: %s", rr.Code, rr.Body.String())
	}
	rr := doUsage(t, s, http.MethodGet, "/api/usage/advice", "", s.handleUsageAdvice)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET advice = %d: %s", rr.Code, rr.Body.String())
	}
	var res usagecalc.AdviceResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode advice: %v", err)
	}
	if res.Advice.Zone != "America/New_York" || !res.Advice.ServerZone {
		t.Errorf("advice zone/server_zone = %q/%v, want America/New_York/true", res.Advice.Zone, res.Advice.ServerZone)
	}
	for name, form := range map[string]string{"cli": res.Text.CLI, "comm": res.Text.Comm} {
		if !strings.Contains(form, "(server zone)") {
			t.Errorf("%s form lacks the server-zone marker:\n%s", name, form)
		}
		if strings.Contains(form, "fell back to UTC") {
			t.Errorf("%s form carries a fallback note it should not:", name)
		}
	}
	got := usageCalcGet(t, s)
	if got["reset_zone"] != "America/New_York" || got["reset_zone_source"] != "server" || got["reset_zone_fell_back"] != false {
		t.Errorf("config block = %#v, want the same server zone marked as such", got)
	}

	// An explicit zone pins the advice too and clears the marker. A second,
	// file-backed server: the config PUT needs a config path to persist to.
	s2 := newUsageCalcServer(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	s2.cfgPath = cfgPath
	putJSON(t, s2.handlePutConfig, "/api/config", map[string]any{usageCalcPrefix + "reset_zone": "Europe/London"})
	rr = doUsage(t, s2, http.MethodGet, "/api/usage/advice", "", s2.handleUsageAdvice)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET advice = %d: %s", rr.Code, rr.Body.String())
	}
	var res2 usagecalc.AdviceResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res2); err != nil {
		t.Fatalf("decode advice: %v", err)
	}
	if res2.Advice.Zone != "Europe/London" || res2.Advice.ServerZone {
		t.Errorf("advice zone/server_zone = %q/%v, want Europe/London/false", res2.Advice.Zone, res2.Advice.ServerZone)
	}
	if strings.Contains(res2.Text.CLI, "(server zone)") {
		t.Errorf("pinned form still carries the server-zone marker:\n%s", res2.Text.CLI)
	}
}

func TestUsageCalcAdviceNotWiredAndMethodNotAllowed(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	if rr := doUsage(t, s, http.MethodGet, "/api/usage/advice", "", s.handleUsageAdvice); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired GET = %d, want 503: %s", rr.Code, rr.Body.String())
	}
	if rr := doUsage(t, s, http.MethodPost, "/api/usage/advice", "", s.handleUsageAdvice); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405: %s", rr.Code, rr.Body.String())
	}
}

func TestUsageCalcRemainingWorkHandler(t *testing.T) {
	s := newUsageCalcServer(t)

	// Set a typed figure, then clear it with an empty points.
	if rr := doUsage(t, s, http.MethodPut, "/api/usage/remaining-work", `{"points": 72}`, s.handleUsageRemainingWork); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	if rr := doUsage(t, s, http.MethodPut, "/api/usage/remaining-work", `{}`, s.handleUsageRemainingWork); rr.Code != http.StatusOK {
		t.Fatalf("PUT clear = %d: %s", rr.Code, rr.Body.String())
	}
	if rr := doUsage(t, s, http.MethodDelete, "/api/usage/remaining-work", "", s.handleUsageRemainingWork); rr.Code != http.StatusOK {
		t.Fatalf("DELETE = %d: %s", rr.Code, rr.Body.String())
	}

	// The figure takes a number; anything else is a client mistake.
	if rr := doUsage(t, s, http.MethodPut, "/api/usage/remaining-work", "not-json", s.handleUsageRemainingWork); rr.Code != http.StatusBadRequest {
		t.Errorf("PUT not-json = %d, want 400: %s", rr.Code, rr.Body.String())
	}
	if rr := doUsage(t, s, http.MethodPut, "/api/usage/remaining-work", `{"points": "many"}`, s.handleUsageRemainingWork); rr.Code != http.StatusBadRequest {
		t.Errorf("PUT points:string = %d, want 400: %s", rr.Code, rr.Body.String())
	}
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if rr := doUsage(t, s, m, "/api/usage/remaining-work", "", s.handleUsageRemainingWork); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405: %s", m, rr.Code, rr.Body.String())
		}
	}

	if rr := doUsage(t, s, http.MethodPut, "/api/usage/remaining-work", `{"points": 40}`, s.handleUsageRemainingWork); rr.Code != http.StatusOK {
		t.Fatalf("re-set = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUsageCalcAdviceAndRemainingWorkCapabilityGates(t *testing.T) {
	s := newUsageCalcServer(t)

	// A caller holding only the write capability: the read endpoints (both
	// advice and the record) refuse it.
	holdWrite := func(method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = withUsageCaps(req, usageCapabilityWrite)
		rr := httptest.NewRecorder()
		switch path {
		case "/api/usage/advice":
			s.handleUsageAdvice(rr, req)
		default:
			s.handleUsageReadings(rr, req)
		}
		return rr.Code
	}
	if got := holdWrite(http.MethodGet, "/api/usage/advice", ""); got != http.StatusForbidden {
		t.Errorf("advice with only usage:write = %d, want 403", got)
	}
	if got := holdWrite(http.MethodGet, "/api/usage/readings", ""); got != http.StatusForbidden {
		t.Errorf("readings with only usage:write = %d, want 403", got)
	}

	// A caller holding only the read capability: the write endpoints refuse it.
	holdRead := func(method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = withUsageCaps(req, usageCapabilityRead)
		rr := httptest.NewRecorder()
		switch path {
		case "/api/usage/remaining-work":
			s.handleUsageRemainingWork(rr, req)
		case "/api/usage/extra-credit":
			s.handleUsageExtraCredit(rr, req)
		default:
			s.handleUsageReadings(rr, req)
		}
		return rr.Code
	}
	if got := holdRead(http.MethodPut, "/api/usage/remaining-work", `{"points": 5}`); got != http.StatusForbidden {
		t.Errorf("remaining-work with only usage:read = %d, want 403", got)
	}
	if got := holdRead(http.MethodPut, "/api/usage/extra-credit", `{"points": 1}`); got != http.StatusForbidden {
		t.Errorf("extra-credit with only usage:read = %d, want 403", got)
	}
	if got := holdRead(http.MethodPost, "/api/usage/readings", `{"source":"typed"}`); got != http.StatusForbidden {
		t.Errorf("POST readings with only usage:read = %d, want 403", got)
	}
	// While both caps together pass the same routes.
	if got := doUsage(t, s, http.MethodGet, "/api/usage/advice", "", s.handleUsageAdvice); got.Code != http.StatusOK {
		t.Errorf("advice with both caps = %d, want 200", got.Code)
	}
}

// adviceWithQuery serves one advice call and decodes the result.
func adviceWithQuery(t *testing.T, s *Server, query string) usagecalc.AdviceResult {
	t.Helper()
	rr := doUsage(t, s, http.MethodGet, "/api/usage/advice"+query, "", s.handleUsageAdvice)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET advice%s = %d: %s", query, rr.Code, rr.Body.String())
	}
	var res usagecalc.AdviceResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode advice: %v", err)
	}
	return res
}

func postUsagePercent(t *testing.T, s *Server, pct float64) {
	t.Helper()
	body := jsonMarshalUsage(t, usagecalc.Reading{At: time.Now().UTC().Add(-time.Minute), Percent: fptrUsage(pct), Source: usagecalc.SourceTyped})
	if rr := doUsage(t, s, http.MethodPost, "/api/usage/readings", body, s.handleUsageReadings); rr.Code != http.StatusOK {
		t.Fatalf("POST reading = %d: %s", rr.Code, rr.Body.String())
	}
}

// TestUsageCalcAdviceLanesWhatIf: the lanes query parameter is a what-if. An explicit
// lanes=0 is honoured and differs from leaving it out, and the lane numbers of the reading
// and the ledger do not move with it.
func TestUsageCalcAdviceLanesWhatIf(t *testing.T) {
	s := newUsageCalcServer(t)
	s.inferenceReg = laneFakeRegistry(t)
	s.SetCapacity(laneFakeLedger(t, "task-a", "task-b"))
	postUsagePercent(t, s, 40) // stamped live with the two Claude lanes in the ledger

	unset := adviceWithQuery(t, s, "")
	zero := adviceWithQuery(t, s, "?lanes=0")
	one := adviceWithQuery(t, s, "?lanes=1")

	if unset.Advice.Lanes != 2 {
		t.Errorf("no lanes parameter: planned with %d lanes, want the stamped 2", unset.Advice.Lanes)
	}
	if zero.Advice.Lanes != 0 || zero.Advice.WhatIfLanes == nil || *zero.Advice.WhatIfLanes != 0 {
		t.Errorf("lanes=0: planned with %d lanes, what-if %v; want an honoured 0", zero.Advice.Lanes, zero.Advice.WhatIfLanes)
	}
	if one.Advice.Lanes != 1 {
		t.Errorf("lanes=1: planned with %d lanes, want 1", one.Advice.Lanes)
	}
	if unset.Advice.WhatIfLanes != nil {
		t.Errorf("no lanes parameter: what-if is %d, want unset", *unset.Advice.WhatIfLanes)
	}
	for name, res := range map[string]usagecalc.AdviceResult{"unset": unset, "zero": zero} {
		if res.Advice.LanesStamped == nil || *res.Advice.LanesStamped != 2 {
			t.Errorf("%s: lanes stamped = %v, want 2", name, res.Advice.LanesStamped)
		}
		if res.Advice.LanesRunningNow == nil || *res.Advice.LanesRunningNow != 2 {
			t.Errorf("%s: lanes running now = %v, want the ledger's 2", name, res.Advice.LanesRunningNow)
		}
		for form, text := range map[string]string{"cli": res.Text.CLI, "comm": res.Text.Comm} {
			if !strings.Contains(text, "Claude lanes running now") || !strings.Contains(text, "Lanes stamped on the newest reading") {
				t.Errorf("%s %s form lacks the lane numbers:\n%s", name, form, text)
			}
		}
	}
	if !strings.Contains(zero.Text.CLI, "What-if lanes (not saved): 0") {
		t.Errorf("lanes=0 CLI form lacks the what-if line:\n%s", zero.Text.CLI)
	}
}

// TestUsageCalcAdviceLanesRunningNow: the ledger counter reaches the service, and the field
// is null while no ledger is wired.
func TestUsageCalcAdviceLanesRunningNow(t *testing.T) {
	s := newUsageCalcServer(t)
	postUsagePercent(t, s, 40)

	rr := doUsage(t, s, http.MethodGet, "/api/usage/advice", "", s.handleUsageAdvice)
	var raw struct {
		Advice map[string]json.RawMessage `json:"advice"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode advice: %v", err)
	}
	if v, ok := raw.Advice["lanes_running_now"]; !ok || string(v) != "null" {
		t.Errorf("no ledger: lanes_running_now = %s (present %v), want null", v, ok)
	}

	s.inferenceReg = laneFakeRegistry(t)
	s.SetCapacity(laneFakeLedger(t, "task-a"))
	if got := adviceWithQuery(t, s, "").Advice.LanesRunningNow; got == nil || *got != 1 {
		t.Errorf("one Claude lease: lanes running now = %v, want 1", got)
	}
}
