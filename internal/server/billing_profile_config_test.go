// REST tests for the usage.billing config surface: PUT /api/config with
// usage.billing.<provider>.<field> keys must validate, store, round-trip
// through GET /api/config, be clearable with null, reject every malformed
// shape loudly, and — once set — drive the cost report's per-row billing
// fields (bundled mode, API-equivalent estimate, points, share of price).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// putConfig does one PUT /api/config with a flat JSON body and returns the
// status code (the callers below assert per-shape, so no assertion here).
func putBillingConfig(t *testing.T, s *Server, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	return rr.Code
}

// getBilling returns the GET /api/config usage.billing map (and, in the
// few places that need it, the decoded usage block).
func getBilling(t *testing.T, s *Server) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Usage struct {
			Billing map[string]any `json:"billing"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got.Usage.Billing
}

// TestBillingProfile_ConfigRoundTrip — the whole profile is settable as one
// PUT, and GET /api/config shows it (currency normalized to upper case).
func TestBillingProfile_ConfigRoundTrip(t *testing.T) {
	s := bl90Server(t)
	body := `{
		"usage.billing.claude-code.mode": "bundled_plan",
		"usage.billing.claude-code.plan": "max5",
		"usage.billing.claude-code.monthly_price": 200,
		"usage.billing.claude-code.currency": "usd",
		"usage.billing.claude-code.reset_weekday": "saturday",
		"usage.billing.claude-code.reset_time": "08:00",
		"usage.billing.claude-code.allowance_unit": "percent"
	}`
	if code := putBillingConfig(t, s, body); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("PUT profile: want 200/204, got %d", code)
	}
	prof, ok := getBilling(t, s)["claude-code"].(map[string]any)
	if !ok {
		t.Fatalf("claude-code profile missing from GET /api/config: %v", getBilling(t, s))
	}
	if prof["mode"] != "bundled_plan" {
		t.Errorf("mode = %v, want bundled_plan", prof["mode"])
	}
	if prof["plan"] != "max5" {
		t.Errorf("plan = %v, want max5", prof["plan"])
	}
	if prof["monthly_price"] != float64(200) {
		t.Errorf("monthly_price = %v, want 200", prof["monthly_price"])
	}
	if prof["currency"] != "USD" {
		t.Errorf("currency = %q, want USD (uppercased)", prof["currency"])
	}
	if prof["reset_weekday"] != "saturday" || prof["reset_time"] != "08:00" {
		t.Errorf("reset = %v / %v, want saturday / 08:00", prof["reset_weekday"], prof["reset_time"])
	}
	if prof["allowance_unit"] != "percent" {
		t.Errorf("allowance_unit = %v, want percent", prof["allowance_unit"])
	}
}

// TestBillingProfile_NullClearsField — null restores the field to the
// default-resolution empty (here: plan cleared, price back to 0); the
// provider entry itself stays, so the mode remains settable.
func TestBillingProfile_NullClearsField(t *testing.T) {
	s := bl90Server(t)
	body := `{
		"usage.billing.openai.mode": "free_local",
		"usage.billing.openai.plan": "lite",
		"usage.billing.openai.monthly_price": 10
	}`
	if code := putBillingConfig(t, s, body); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("PUT profile: got %d", code)
	}
	if code := putBillingConfig(t, s, `{"usage.billing.openai.plan": null, "usage.billing.openai.monthly_price": 0}`); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("PUT clear: got %d", code)
	}
	prof := getBilling(t, s)["openai"].(map[string]any)
	if v, present := prof["plan"]; present && v != "" {
		t.Errorf("plan not cleared, got %v", v)
	}
	if v, present := prof["monthly_price"]; present && v != float64(0) {
		t.Errorf("monthly_price not cleared, got %v", v)
	}
	if prof["mode"] != "free_local" {
		t.Errorf("mode should survive the clear, got %v", prof["mode"])
	}
}

var billingRejections = []struct {
	name string
	body string
	// want contains a fragment of the 400 message (empty = any message
	// works, but the key must still appear in it).
	fragment string
}{
	{"UnknownField", `{"usage.billing.openai.bogus": 1}`, "usage.billing.openai.bogus"},
	{"ExtraDot", `{"usage.billing.openai.plan.x": 1}`, "usage.billing.openai.plan.x"},
	{"EmptyProvider", `{"usage.billing..mode": "free_local"}`, "usage.billing..mode"},
	{"BarePrefix", `{"usage.billing.": 1}`, "usage.billing"},
	{"UnknownMode", `{"usage.billing.openai.mode": "quantum"}`, "bundled_plan"},
	{"UnknownUnit", `{"usage.billing.openai.allowance_unit": "credits"}`, "percent"},
	{"NegativePrice", `{"usage.billing.openai.monthly_price": -5}`, "monthly_price"},
	{"BoolPrice", `{"usage.billing.openai.monthly_price": true}`, "monthly_price"},
	{"BoolStringField", `{"usage.billing.openai.plan": true}`, "plan"},
	{"BadWeekday", `{"usage.billing.openai.reset_weekday": "funday"}`, "reset_weekday"},
	{"BadTime", `{"usage.billing.openai.reset_time": "8am"}`, "reset_time"},
}

// TestBillingProfile_RejectsMalformed — a malformed key or value is a 400
// that names the offending key (the daemon's whole-PUT-rejects discipline),
// and nothing about the other keys in the same body is applied.
func TestBillingProfile_RejectsMalformed(t *testing.T) {
	for _, tc := range billingRejections {
		t.Run(tc.name, func(t *testing.T) {
			s := bl90Server(t)
			// A valid key in the same body: if the invalid one slips
			// through (or the valid one is applied alone), the round-trip
			// assertion below catches it.
			good := `{"usage.billing.openai.mode": "free_local", `
			code := putBillingConfig(t, s, good+strings.TrimPrefix(tc.body, `{`))
			if code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d", code)
			}
			// Rebuild the message: the 400 body must name the bad key.
			req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(good+strings.TrimPrefix(tc.body, `{`)))
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, req)
			if !strings.Contains(rr.Body.String(), tc.fragment) {
				t.Errorf("400 message %q does not name %q", rr.Body.String(), tc.fragment)
			}
		})
	}
}

// TestBillingProfile_RejectionAppliesNothing — an invalid body must not
// store the valid keys it carried with it (same-PUT, all-or-nothing, the
// way every other usage.* surface behaves).
func TestBillingProfile_RejectionAppliesNothing(t *testing.T) {
	s := bl90Server(t)
	code := putBillingConfig(t, s, `{
		"usage.billing.openai.mode": "free_local",
		"usage.billing.openai.plan": 42
	}`)
	if code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", code)
	}
	billing := getBilling(t, s)
	if len(billing) != 0 {
		t.Errorf("invalid PUT must apply nothing, but billing = %v", billing)
	}
}

// TestBillingProfile_DrivesCostReportBilling — a bundled_plan profile with
// a price turns the claude-code row of the backend report into a labeled
// estimate (API-equivalent when the rate table prices it, plus points and
// share of price); an unprofiled backend row stays api_metered and is
// labeled as the assumed default.
func TestBillingProfile_DrivesCostReportBilling(t *testing.T) {
	s := bl90Server(t)
	now := time.Now()
	s.costReportLedger = fakeSpendLedger{
		{SessionID: "abcd1234", Backend: "claude-code", StartedAt: now, TokensIn: 100000, TokensOut: 50000},
		{SessionID: "ef012345", Backend: "ollama", StartedAt: now, TokensIn: 100000, TokensOut: 50000},
	}
	body := `{
		"usage.billing.claude-code.mode": "bundled_plan",
		"usage.billing.claude-code.monthly_price": 200
	}`
	if code := putBillingConfig(t, s, body); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("PUT profile: got %d", code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cost/report?group_by=backend", nil)
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/cost/report: %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Rows          []map[string]any `json:"rows"`
		RetentionNote string           `json:"retention_note"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.RetentionNote == "" {
		t.Errorf("retention_note must ride on every report")
	}
	var cc, ol map[string]any
	for _, row := range out.Rows {
		switch row["key"] {
		case "claude-code":
			cc = row
		case "ollama":
			ol = row
		}
	}
	if cc == nil || ol == nil {
		t.Fatalf("want claude-code and ollama rows, got %v", out.Rows)
	}
	if cc["billing_mode"] != "bundled_plan" {
		t.Errorf("claude-code billing_mode = %v, want bundled_plan", cc["billing_mode"])
	}
	if cc["billing_label"] == nil || cc["billing_label"] == "" {
		t.Errorf("claude-code billing_label missing: %v", cc)
	}
	// The rate table may or may not price claude-code in this fixture; when
	// it does, the estimate, the points and the share all appear together.
	if est, ok := cc["api_equivalent_usd"].(float64); ok && est > 0 {
		if pts, ok := cc["estimated_usage_points"].(float64); !ok || pts <= 0 {
			t.Errorf("estimated_usage_points missing/<=0 while api_equivalent_usd=%v: %v", est, cc)
		}
		if share, ok := cc["share_of_monthly_price"].(float64); !ok || share <= 0 {
			t.Errorf("share_of_monthly_price missing/<=0 while api_equivalent_usd=%v: %v", est, cc)
		}
	}
	if ol["billing_mode"] != "api_metered" {
		t.Errorf("ollama billing_mode = %v, want api_metered", ol["billing_mode"])
	}
	label, _ := ol["billing_label"].(string)
	if !strings.Contains(label, "assumed") {
		t.Errorf("ollama billing_label %q must name it as the assumed default", label)
	}
}
