package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// triageSpy is the double for the triage read route: a fixed record list
// and a fixed alert threshold, plus a count of read calls so the tests
// can prove a refused request never reaches the manager.
type triageSpy struct {
	fakeOrchAutonomous
	records []*autonomous.TriageRecord
	alert   int
	calls   int
}

func (s *triageSpy) ListTriageRecords() []*autonomous.TriageRecord {
	s.calls++
	return s.records
}

func (s *triageSpy) TriageAlertMinutes() int { return s.alert }

func triageGET(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleAutonomousTriage(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/triage"+query, nil))
	return rec
}

func triageBaseTime() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }

// triageFixtureRecords is the store's content: two records of one
// Automation (one failed, one blocked) and one of another. Their refresh
// times differ on purpose — the route must return them newest first
// whatever order the store yields them.
func triageFixtureRecords() []*autonomous.TriageRecord {
	base := triageBaseTime()
	return []*autonomous.TriageRecord{
		{
			AutomationID: "auto-b",
			PRDID:        "prd-c", PRDTitle: "Ingest",
			Status:      string(autonomous.PRDFailed),
			Error:       "worker exited with 1",
			FirstSeenAt: base, UpdatedAt: base,
		},
		{
			AutomationID: "auto-a", AutomationName: "Monthly report",
			PRDID: "prd-b", PRDTitle: "Sync",
			Status:        string(autonomous.PRDBlocked),
			BlockedReason: "waiting on operator approval",
			FirstSeenAt:   base, UpdatedAt: base.Add(time.Minute),
		},
		{
			AutomationID: "auto-a", AutomationName: "Monthly report",
			PRDID: "prd-a", PRDTitle: "Reports",
			Status: string(autonomous.PRDFailed),
			TaskID: "t-a", TaskTitle: "Build report", Error: "build failed",
			FirstSeenAt: base, UpdatedAt: base.Add(2 * time.Minute),
			Notes: []autonomous.TriageNote{{At: base.Add(time.Minute), Source: "alert", Text: "still waiting"}},
		},
	}
}

func triageFixtureServer() (*Server, *triageSpy) {
	spy := &triageSpy{records: triageFixtureRecords(), alert: autonomous.DefaultAutomationTriageAlertMinutes}
	return &Server{autonomousMgr: spy}, spy
}

func triageRecords(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var got struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	return got.Records
}

func TestTriageReadShape(t *testing.T) {
	s, _ := triageFixtureServer()
	rec := triageGET(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	var body struct {
		Records      []map[string]any `json:"records"`
		AlertMinutes int              `json:"alert_minutes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body)
	}
	if len(body.Records) != 3 {
		t.Fatalf("records = %d, want all three stored records", len(body.Records))
	}
	if body.AlertMinutes != autonomous.DefaultAutomationTriageAlertMinutes {
		t.Errorf("alert_minutes = %d, want the default of %d", body.AlertMinutes, autonomous.DefaultAutomationTriageAlertMinutes)
	}
	first := body.Records[0]
	for _, key := range []string{"prd_id", "status", "automation_id", "first_seen_at", "updated_at", "retry_budget"} {
		if _, ok := first[key]; !ok {
			t.Errorf("record lacks %q: %v", key, first)
		}
	}
	// The newest record (prd-a) is back first, with everything the reader
	// needs beside it.
	if first["prd_id"] != "prd-a" || first["status"] != string(autonomous.PRDFailed) {
		t.Errorf("newest first = prd %v (%v); want prd-a failed", first["prd_id"], first["status"])
	}
}

func TestTriageReadNewestFirst(t *testing.T) {
	s, _ := triageFixtureServer()
	rec := triageGET(t, s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	got := []string{}
	for _, r := range triageRecords(t, rec.Body.Bytes()) {
		got = append(got, r["prd_id"].(string))
	}
	want := []string{"prd-a", "prd-b", "prd-c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("record order = %v, want newest first %v", got, want)
	}
}

func TestTriageReadFilters(t *testing.T) {
	cases := []struct {
		query                         string
		wantCount                     int
		wantPRD, wantAuto, wantStatus string
	}{
		{`?automation_id=auto-a`, 2, "", "auto-a", ""},
		{`?prd_id=prd-b`, 1, "prd-b", "", ""},
		{`?status=failed`, 2, "", "", "failed"},
		{`?status=blocked`, 1, "", "", "blocked"},
		{`?automation_id=auto-a&status=failed`, 1, "prd-a", "auto-a", "failed"},
		{`?automation_id=auto-a&prd_id=prd-b&status=blocked`, 1, "prd-b", "auto-a", "blocked"},
		{`?status=&x=ignored`, 3, "", "", ""},
	}
	for _, c := range cases {
		t.Run("query="+c.query, func(t *testing.T) {
			s, _ := triageFixtureServer()
			rec := triageGET(t, s, c.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
			}
			records := triageRecords(t, rec.Body.Bytes())
			if len(records) != c.wantCount {
				t.Fatalf("records = %d, want %d: %v", len(records), c.wantCount, records)
			}
			for _, r := range records {
				if c.wantPRD != "" && r["prd_id"] != c.wantPRD {
					t.Errorf("record out of the filter: %v", r)
				}
				if c.wantAuto != "" && r["automation_id"] != c.wantAuto {
					t.Errorf("record out of the filter: %v", r)
				}
				if c.wantStatus != "" && r["status"] != c.wantStatus {
					t.Errorf("record out of the filter: %v", r)
				}
			}
		})
	}
}

func TestTriageReadBadStatusIsRefused(t *testing.T) {
	for _, query := range []string{`?status=pending`, `?status=Failed`, `?status=FAILED`} {
		s, spy := triageFixtureServer()
		rec := triageGET(t, s, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: want 400, got %d: %s", query, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `status must be "failed" or "blocked"`) {
			t.Errorf("query %q: refusal lost its explanation: %s", query, rec.Body.String())
		}
		if spy.calls != 0 {
			t.Errorf("query %q: a refused status must not reach the manager", query)
		}
	}
}

func TestTriageReadMatchesNothingListsNothing(t *testing.T) {
	s, _ := triageFixtureServer()
	rec := triageGET(t, s, `?automation_id=nobody`)
	if rec.Code != http.StatusOK { // a read that matches nothing is an empty list, not a 404
		t.Fatalf("want 200 with an empty list, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"records":[]`) {
		t.Errorf("body lacks the empty list: %s", body)
	}
	all := rec.Body.String()
	if strings.Contains(all, "prd-a") {
		t.Errorf("a filter that matches nothing must yield no records: %s", all)
	}

	empty := &Server{autonomousMgr: &triageSpy{alert: 5}}
	rec = triageGET(t, empty, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("an empty store must list, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"records":[]`) || !strings.Contains(body, `"alert_minutes":5`) {
		t.Errorf("empty store body: %s", body)
	}
}

func TestTriageReadAlertMinutesFollowsConfig(t *testing.T) {
	cases := []struct {
		configured int
		want       int
	}{
		{autonomous.DefaultAutomationTriageAlertMinutes, autonomous.DefaultAutomationTriageAlertMinutes},
		{0, 0}, // an explicit 0 (alert off) must be reported, not read as unset
		{45, 45},
	}
	for _, c := range cases {
		spy := &triageSpy{records: []*autonomous.TriageRecord{}, alert: c.configured}
		s := &Server{autonomousMgr: spy}
		rec := triageGET(t, s, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("alert %d: want 200, got %d", c.configured, rec.Code)
		}
		var body struct {
			AlertMinutes int `json:"alert_minutes"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.AlertMinutes != c.want {
			t.Errorf("configured %d: alert_minutes = %d, want %d", c.configured, body.AlertMinutes, c.want)
		}
	}
}

func TestTriageReadMethodGuard(t *testing.T) {
	s, spy := triageFixtureServer()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleAutonomousTriage(rec, httptest.NewRequest(method, "/api/autonomous/triage", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	if spy.calls != 0 {
		t.Errorf("refused methods must not reach the manager, got %d calls", spy.calls)
	}
}

func TestTriageReadDeniedWithoutCapability(t *testing.T) {
	s, _ := triageFixtureServer()
	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/triage", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleAutonomousTriage(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/triage", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleAutonomousTriage(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the read capability: want 200, got %d", rec.Code)
	}
}

func TestTriageReadAutonomousDisabled(t *testing.T) {
	s := &Server{}
	rec := triageGET(t, s, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 with the manager down, got %d", rec.Code)
	}
}
