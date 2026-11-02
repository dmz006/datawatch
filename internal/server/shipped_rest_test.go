package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// shippedTestAt is the fixed merge time the REST shipped tests record.
var shippedTestAt = time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)

// shipRESTSpy embeds the interface stub and captures the two shipped-record
// calls. For known ids it returns a prebuilt entity with the four shipped
// fields set and Status untouched; for unknown ids the record errors so the
// handler's not-found path can be exercised.
type shipRESTSpy struct {
	fakeOrchAutonomous

	prdKnown  map[string]bool
	autoKnown map[string]bool

	prdRelease, prdCommit, prdSource string
	prdAt                            time.Time
	prdCalls                         int

	autoRelease, autoCommit, autoSource string
	autoAt                              time.Time
	autoCalls                           int

	prdErr  error
	autoErr error
}

func newShipSpy() *shipRESTSpy {
	return &shipRESTSpy{
		prdKnown:  map[string]bool{"p1": true},
		autoKnown: map[string]bool{"a1": true},
	}
}

// GetPRD reports presence so the PRD handler's not-found check can be
// steered: known ids map to 400 (found but the record failed), unknown ids
// to 404.
func (s *shipRESTSpy) GetPRD(id string) (any, bool) {
	return nil, s.prdKnown[id]
}

func (s *shipRESTSpy) RecordPRDShipped(id, release string, mergedAt time.Time, commit, source string) (any, error) {
	s.prdCalls++
	s.prdRelease, s.prdAt, s.prdCommit, s.prdSource = release, mergedAt, commit, source
	if s.prdErr != nil {
		return nil, s.prdErr
	}
	if !s.prdKnown[id] {
		return nil, errors.New("prd not found")
	}
	at := mergedAt
	// Status is a fixed "completed"; a correct record leaves it exactly there.
	return &autonomous.PRD{
		ID:              id,
		Status:          autonomous.PRDCompleted,
		MergedInRelease: release,
		MergedAt:        &at,
		MergeCommit:     commit,
		ShippedSource:   source,
	}, nil
}

func (s *shipRESTSpy) RecordAutomationShipped(id, release string, mergedAt time.Time, commit, source string) (any, error) {
	s.autoCalls++
	s.autoRelease, s.autoAt, s.autoCommit, s.autoSource = release, mergedAt, commit, source
	if s.autoErr != nil {
		return nil, s.autoErr
	}
	if !s.autoKnown[id] {
		return nil, errors.New("404: automation " + id + " not found")
	}
	at := mergedAt
	return &autonomous.Automation{
		ID:              id,
		Name:            "shipped-rest",
		Status:          autonomous.AutomationPaused,
		MergedInRelease: release,
		MergedAt:        &at,
		MergeCommit:     commit,
		ShippedSource:   source,
	}, nil
}

func postShippedPRD(s *Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestShippedREST_RecordPRD(t *testing.T) {
	spy := newShipSpy()
	s := &Server{autonomousMgr: spy}
	rec := postShippedPRD(s, "/api/autonomous/prds/p1/shipped",
		`{"merged_in_release":"v1.2.0","merged_at":"2026-11-20T09:30:00Z","merge_commit":"c0ffee42","source":"backfill"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The four record arguments reached the manager exactly.
	if spy.prdCalls != 1 {
		t.Fatalf("RecordPRDShipped called %d times, want 1", spy.prdCalls)
	}
	if spy.prdRelease != "v1.2.0" || spy.prdCommit != "c0ffee42" || spy.prdSource != "backfill" {
		t.Errorf("captured PRD args = release %q commit %q source %q; want v1.2.0 c0ffee42 backfill",
			spy.prdRelease, spy.prdCommit, spy.prdSource)
	}
	if !spy.prdAt.Equal(shippedTestAt) {
		t.Errorf("captured merged_at = %v, want %v", spy.prdAt, shippedTestAt)
	}
	// The reply carries the shipped state and the untouched status.
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("reply is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got["id"] != "p1" || got["status"] != "completed" {
		t.Errorf("reply id/status = %v/%v; want p1/completed (status must not move)", got["id"], got["status"])
	}
	if got["merged_in_release"] != "v1.2.0" || got["merge_commit"] != "c0ffee42" || got["shipped_source"] != "backfill" {
		t.Errorf("reply shipped fields = %v/%v/%v; want v1.2.0 c0ffee42 backfill",
			got["merged_in_release"], got["merge_commit"], got["shipped_source"])
	}
	if got["merged_at"] != shippedTestAt.Format(time.RFC3339) {
		t.Errorf("reply merged_at = %v, want %v", got["merged_at"], shippedTestAt)
	}
}

func TestShippedREST_RecordAutomation(t *testing.T) {
	spy := newShipSpy()
	s := &Server{autonomousMgr: spy}
	rec := httptest.NewRecorder()
	body := `{"merged_in_release":"v3.0.0","merged_at":"2026-11-20T09:30:00Z","merge_commit":"bead1","source":"merge_step"}`
	s.handleAutomationShipped(rec, httptest.NewRequest(http.MethodPost, "/api/automations/a1/shipped", strings.NewReader(body)), "a1")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if spy.autoCalls != 1 {
		t.Fatalf("RecordAutomationShipped called %d times, want 1", spy.autoCalls)
	}
	if spy.autoRelease != "v3.0.0" || spy.autoCommit != "bead1" || spy.autoSource != "merge_step" {
		t.Errorf("captured automation args = release %q commit %q source %q; want v3.0.0 bead1 merge_step",
			spy.autoRelease, spy.autoCommit, spy.autoSource)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("reply is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got["id"] != "a1" || got["status"] != "paused" {
		t.Errorf("reply id/status = %v/%v; want a1/paused (status must not move)", got["id"], got["status"])
	}
	if got["merged_in_release"] != "v3.0.0" || got["merge_commit"] != "bead1" || got["shipped_source"] != "merge_step" {
		t.Errorf("reply shipped fields = %v/%v/%v; want v3.0.0 bead1 merge_step",
			got["merged_in_release"], got["merge_commit"], got["shipped_source"])
	}
}

func TestShippedREST_MethodNotAllowed(t *testing.T) {
	spy := newShipSpy()
	s := &Server{autonomousMgr: spy}

	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/p1/shipped", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PRD GET shipped: want 405, got %d", rec.Code)
	}
	if spy.prdCalls != 0 {
		t.Errorf("PRD GET shipped still called the record: %d", spy.prdCalls)
	}

	rec = httptest.NewRecorder()
	s.handleAutomationShipped(rec, httptest.NewRequest(http.MethodGet, "/api/automations/a1/shipped", nil), "a1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("automation GET shipped: want 405, got %d", rec.Code)
	}
	if spy.autoCalls != 0 {
		t.Errorf("automation GET shipped still called the record: %d", spy.autoCalls)
	}
}

func TestShippedREST_NotFound(t *testing.T) {
	spy := newShipSpy()
	spy.prdErr = errors.New("record refused")
	// p1 is known -> the PRD handler reports the refusal as 400 (found but
	// rejected); an unknown id would 404.
	rec := postShippedPRD(&Server{autonomousMgr: spy}, "/api/autonomous/prds/p1/shipped", `{"merge_commit":"c1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("PRD shipped, known id + record error: want 400, got %d", rec.Code)
	}

	spy2 := newShipSpy()
	spy2.prdKnown["zzz"] = false
	rec2 := postShippedPRD(&Server{autonomousMgr: spy2}, "/api/autonomous/prds/zzz/shipped", `{"merge_commit":"c1"}`)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("PRD shipped, unknown id: want 404, got %d", rec2.Code)
	}

	spy3 := newShipSpy()
	rec3 := httptest.NewRecorder()
	srv3 := &Server{autonomousMgr: spy3}
	srv3.handleAutomationShipped(rec3, httptest.NewRequest(http.MethodPost, "/api/automations/nope/shipped", strings.NewReader(`{"merge_commit":"c1"}`)), "nope")
	if rec3.Code != http.StatusNotFound {
		t.Errorf("automation shipped, unknown id: want 404, got %d", rec3.Code)
	}
}
