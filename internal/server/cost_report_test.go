// Cost report REST tests (internal/costreport over the server API).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCostReport_ConfigRoundTrip — PUT cost_report.default_group_by, GET
// /api/config returns the same value.
func TestCostReport_ConfigRoundTrip(t *testing.T) {
	s := bl90Server(t)

	body := `{"cost_report.default_group_by":"backend"}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusNoContent {
		t.Fatalf("PUT /api/config status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr = httptest.NewRecorder()
	s.handleGetConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	cr, ok := got["cost_report"].(map[string]any)
	if !ok {
		t.Fatalf("cost_report section missing from GET: %v", got)
	}
	if cr["default_group_by"] != "backend" {
		t.Errorf("default_group_by not round-tripped: %v", cr["default_group_by"])
	}
}

// TestCostReport_InvalidGroupBy — an invalid group_by is rejected with
// HTTP 400.
func TestCostReport_InvalidGroupBy(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cost/report?group_by=quarterly", nil)
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestCostReport_ValidGroupBy — a valid group_by returns the engine's
// report with retention_days set.
func TestCostReport_ValidGroupBy(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cost/report?group_by=backend", nil)
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var report struct {
		GroupBy          string `json:"group_by"`
		Rows             []any  `json:"rows"`
		RetentionDays    int    `json:"retention_days"`
		LifetimeFallback bool   `json:"lifetime_fallback"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.GroupBy != "backend" {
		t.Errorf("group_by = %q, want backend", report.GroupBy)
	}
	if report.RetentionDays != 31 {
		t.Errorf("retention_days = %d, want 31", report.RetentionDays)
	}
}

// TestCostReport_InvalidWindowBound — an unparseable since bound is 400.
func TestCostReport_InvalidWindowBound(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cost/report?since=yesterday", nil)
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestCostReport_RejectsNonGet — anything other than GET is 405.
func TestCostReport_RejectsNonGet(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/cost/report", nil)
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
}
