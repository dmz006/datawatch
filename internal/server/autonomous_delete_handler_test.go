// v6.13.6 — operator-reported: "Delete function says it deleted the
// selected automata but it didn't, check all multi-select button
// actions work, there should have been tests".
//
// Bug: the PWA batchAutomataAction was POSTing DELETE to
// /api/autonomous/prds/{id} *without* `?hard=true`. The handler treats
// a bare DELETE as Cancel (legacy v4.0 behavior); only `?hard=true`
// invokes DeletePRD. Toast said "deleted" but the PRD remained.
//
// These tests pin the HTTP-layer contract so the regression can't
// silently come back: bare DELETE → Cancel; DELETE?hard=true → DeletePRD.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// spyAutonomous embeds the existing fakeOrchAutonomous and adds counters
// for Cancel + DeletePRD so we can assert which one fired.
type spyAutonomous struct {
	fakeOrchAutonomous

	cancelCalls    []string
	deleteCalls    []string
	cancelErr      error
	deleteErr      error
}

func (s *spyAutonomous) Cancel(id string) error {
	s.cancelCalls = append(s.cancelCalls, id)
	return s.cancelErr
}
func (s *spyAutonomous) DeletePRD(id string) error {
	s.deleteCalls = append(s.deleteCalls, id)
	return s.deleteErr
}
// Override SessionIDsForPRD so the kill-sessions hook is a no-op (no
// manager wired in this test).
func (s *spyAutonomous) SessionIDsForPRD(string) []string { return nil }

func newDeleteHandlerTestServer(spy *spyAutonomous) *Server {
	return &Server{autonomousMgr: spy}
}

// Bare DELETE — the legacy v4.0 contract: status flips to cancelled
// (Manager.Cancel is invoked, NOT DeletePRD).
func TestAutonomousPRDs_DELETE_BareIsCancel(t *testing.T) {
	spy := &spyAutonomous{}
	s := newDeleteHandlerTestServer(spy)

	req := httptest.NewRequest(http.MethodDelete, "/api/autonomous/prds/prd-abc", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("bare DELETE: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := spy.cancelCalls; len(got) != 1 || got[0] != "prd-abc" {
		t.Errorf("bare DELETE should invoke Cancel exactly once for prd-abc, got %v", got)
	}
	if len(spy.deleteCalls) != 0 {
		t.Errorf("bare DELETE must NOT invoke DeletePRD, got %v", spy.deleteCalls)
	}
}

// DELETE with ?hard=true — actual hard-delete (DeletePRD invoked,
// Cancel not). This is the path the PWA's multi-select Delete button
// MUST hit. Pre-v6.13.6 it omitted the query param and silently
// down-graded to Cancel — operator saw "deleted" toast but the PRD
// remained in the list.
func TestAutonomousPRDs_DELETE_HardTrueIsDeletePRD(t *testing.T) {
	spy := &spyAutonomous{}
	s := newDeleteHandlerTestServer(spy)

	req := httptest.NewRequest(http.MethodDelete, "/api/autonomous/prds/prd-xyz?hard=true", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("hard DELETE: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := spy.deleteCalls; len(got) != 1 || got[0] != "prd-xyz" {
		t.Errorf("hard DELETE should invoke DeletePRD exactly once for prd-xyz, got %v", got)
	}
	if len(spy.cancelCalls) != 0 {
		t.Errorf("hard DELETE must NOT invoke Cancel, got %v", spy.cancelCalls)
	}
}

// DELETE with hard=false — explicit "false" must still be treated as
// the bare/cancel path (only literal "true" triggers hard-delete).
func TestAutonomousPRDs_DELETE_HardFalseIsCancel(t *testing.T) {
	spy := &spyAutonomous{}
	s := newDeleteHandlerTestServer(spy)

	req := httptest.NewRequest(http.MethodDelete, "/api/autonomous/prds/prd-soft?hard=false", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("hard=false DELETE: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(spy.cancelCalls) != 1 || spy.cancelCalls[0] != "prd-soft" {
		t.Errorf("hard=false DELETE should invoke Cancel, got cancelCalls=%v", spy.cancelCalls)
	}
	if len(spy.deleteCalls) != 0 {
		t.Errorf("hard=false DELETE must NOT invoke DeletePRD, got %v", spy.deleteCalls)
	}
}
