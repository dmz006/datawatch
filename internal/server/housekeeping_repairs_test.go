package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

type housekeepingRepairsSpy struct {
	fakeOrchAutonomous
	view  autonomous.HousekeepingRepairsView
	calls int
}

func (s *housekeepingRepairsSpy) HousekeepingRepairsView() autonomous.HousekeepingRepairsView {
	s.calls++
	return s.view
}

func housekeepingRepairsServer() (*Server, *housekeepingRepairsSpy) {
	spy := &housekeepingRepairsSpy{view: autonomous.HousekeepingRepairsView{
		GraceSeconds: 120,
		Groups: []autonomous.HousekeepingRepairGroupView{{
			Group: "task_chain", Mode: "report",
			Would: []autonomous.RepairRecord{{Group: "task_chain", Subject: "t1", Kind: "would", Found: "stuck", Undo: "reset it"}},
		}},
	}}
	return &Server{autonomousMgr: spy}, spy
}

func TestHousekeepingRepairsREST_Served(t *testing.T) {
	s, spy := housekeepingRepairsServer()
	rec := httptest.NewRecorder()
	s.handleHousekeepingRepairs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/housekeeping-repairs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var got autonomous.HousekeepingRepairsView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.GraceSeconds != 120 || len(got.Groups) != 1 || got.Groups[0].Mode != "report" || len(got.Groups[0].Would) != 1 {
		t.Errorf("body = %+v", got)
	}
	if spy.calls != 1 {
		t.Errorf("calls = %d", spy.calls)
	}
}

func TestHousekeepingRepairsREST_ReadOnly(t *testing.T) {
	s, spy := housekeepingRepairsServer()
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleHousekeepingRepairs(rec, httptest.NewRequest(m, "/api/autonomous/housekeeping-repairs", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code = %d, want 405", m, rec.Code)
		}
	}
	if spy.calls != 0 {
		t.Errorf("refused methods reached the manager: %d", spy.calls)
	}
}

func TestHousekeepingRepairsREST_CapabilityAndDisabled(t *testing.T) {
	s, _ := housekeepingRepairsServer()
	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/housekeeping-repairs", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleHousekeepingRepairs(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("want 403 naming the capability, got %d %q", rec.Code, rec.Body.String())
	}
	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/housekeeping-repairs", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleHousekeepingRepairs(rec, allow)
	if rec.Code != http.StatusOK {
		t.Errorf("with capability: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	(&Server{}).handleHousekeepingRepairs(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled: %d", rec.Code)
	}
}
