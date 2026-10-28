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
