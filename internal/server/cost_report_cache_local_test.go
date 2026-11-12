package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

)

// The REST body carries totals with cache and local tokens, and omits the
// reconciliation when no usage readings are wired.
func TestCostReport_TotalsAndNoReconciliationWithoutReadings(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cost/report?group_by=backend", nil)
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var tot map[string]int
	if err := json.Unmarshal(body["totals"], &tot); err != nil {
		t.Fatalf("totals: %v", err)
	}
	for _, k := range []string{"sessions", "tokens_in", "tokens_out", "cache_read", "cache_creation", "local_tokens"} {
		if _, ok := tot[k]; !ok {
			t.Errorf("totals lacks %q", k)
		}
	}
	if _, ok := body["reconciliation"]; ok {
		t.Error("reconciliation must be absent without readings")
	}
}

func TestCostReportUsageInput_NilWithoutCalculator(t *testing.T) {
	s := bl90Server(t)
	if got := s.costReportUsageInput(t.Context(), time.Now()); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}
