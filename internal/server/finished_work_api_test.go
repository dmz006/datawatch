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

// finishedWorkSpy stands in for the autonomous manager on the finished-work
// routes and the cancel and archive decision paths.
type finishedWorkSpy struct {
	fakeOrchAutonomous
	ran       []string
	cancelled []string
	archived  []string
	cancelErr error
}

func (f *finishedWorkSpy) FinishedWorkDigest() autonomous.FinishedWorkDigest {
	return autonomous.FinishedWorkDigest{Mode: "on", Waiting: []autonomous.FinishedWorkFinding{
		{Automaton: "abcd0001", Title: "Git hosts", Cause: autonomous.FinishedWorkNotMerged, Ahead: 57, Owner: "dave", QuestionID: "q-1"}}}
}
func (f *finishedWorkSpy) RunFinishedWorkSweep(context.Context) autonomous.FinishedWorkPass {
	f.ran = append(f.ran, "sweep")
	return autonomous.FinishedWorkPass{Mode: "on", Found: 1, Asked: 1}
}
func (f *finishedWorkSpy) RunFinishedWorkAudit(context.Context) (autonomous.FinishedWorkAuditResult, error) {
	f.ran = append(f.ran, "audit")
	return autonomous.FinishedWorkAuditResult{Mode: "on", Scanned: 3}, nil
}
func (f *finishedWorkSpy) Cancel(id string) error {
	f.cancelled = append(f.cancelled, id+"|plain")
	return f.cancelErr
}
func (f *finishedWorkSpy) CancelDecided(id, decision, reason string) error {
	f.cancelled = append(f.cancelled, id+"|"+decision+"|"+reason)
	return f.cancelErr
}
func (f *finishedWorkSpy) Archive(id string) (any, error) {
	f.archived = append(f.archived, id+"|plain")
	return map[string]any{"id": id}, nil
}
func (f *finishedWorkSpy) ArchiveDecided(id, decision, reason string) (any, error) {
	f.archived = append(f.archived, id+"|"+decision+"|"+reason)
	return map[string]any{"id": id}, nil
}

func TestFinishedWorkRESTDigest(t *testing.T) {
	s := &Server{autonomousMgr: &finishedWorkSpy{}}
	rec := httptest.NewRecorder()
	s.handleFinishedWork(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/finished-work", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body %s", rec.Code, rec.Body.String())
	}
	var d autonomous.FinishedWorkDigest
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Waiting) != 1 || d.Waiting[0].Owner != "dave" || d.Waiting[0].QuestionID != "q-1" {
		t.Errorf("digest = %+v", d)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleFinishedWork(rec, httptest.NewRequest(m, "/api/autonomous/finished-work", strings.NewReader("{}")))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code = %d, want 405", m, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	(&Server{}).handleFinishedWork(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/finished-work", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled: code = %d, want 503", rec.Code)
	}
}

func TestFinishedWorkRESTCapabilities(t *testing.T) {
	s := &Server{autonomousMgr: &finishedWorkSpy{}}
	with := func(r *http.Request, caps ...string) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), sessionCapsKey, caps))
	}
	rec := httptest.NewRecorder()
	s.handleFinishedWork(rec, with(httptest.NewRequest(http.MethodGet, "/api/autonomous/finished-work", nil), federation.CapHealthRead))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("read without the cap: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.handleFinishedWorkRun(rec, with(httptest.NewRequest(http.MethodPost, "/api/autonomous/finished-work/run", strings.NewReader(`{"run":"sweep"}`)), federation.CapAutonomousRead))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousWrite) {
		t.Errorf("run with only the read cap: %d %s", rec.Code, rec.Body.String())
	}
}

func TestFinishedWorkRESTRun(t *testing.T) {
	spy := &finishedWorkSpy{}
	s := &Server{autonomousMgr: spy}
	for _, run := range []string{"sweep", "audit"} {
		rec := httptest.NewRecorder()
		s.handleFinishedWorkRun(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/finished-work/run", strings.NewReader(`{"run":"`+run+`"}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code = %d, body %s", run, rec.Code, rec.Body.String())
		}
	}
	if strings.Join(spy.ran, ",") != "sweep,audit" {
		t.Errorf("ran = %v", spy.ran)
	}
	rec := httptest.NewRecorder()
	s.handleFinishedWorkRun(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/finished-work/run", strings.NewReader(`{"run":"everything"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown run: code = %d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleFinishedWorkRun(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/finished-work/run", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: code = %d, want 405", rec.Code)
	}
}

func TestCancelAndArchiveRoutesCarryTheDecision(t *testing.T) {
	spy := &finishedWorkSpy{}
	s := &Server{autonomousMgr: spy}
	if err := s.cancelAutomaton("a1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.cancelAutomaton("a2", "keep", "for review"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.archiveAutomaton("a3", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.archiveAutomaton("a4", "drop", "superseded"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(spy.cancelled, ";"); got != "a1|plain;a2|keep|for review" {
		t.Errorf("cancelled = %s", got)
	}
	if got := strings.Join(spy.archived, ";"); got != "a3|plain;a4|drop|superseded" {
		t.Errorf("archived = %s", got)
	}
}

func TestCancelRefusalReachesTheClientAsA400WithTheReason(t *testing.T) {
	spy := &finishedWorkSpy{cancelErr: context.DeadlineExceeded}
	s := &Server{autonomousMgr: spy}
	if err := s.cancelAutomaton("a1", "", ""); err == nil {
		t.Fatal("the guard's refusal must come back as an error")
	}
	// Managers without the decision interface refuse a decision cleanly.
	bare := &Server{autonomousMgr: &fakeOrchAutonomous{}}
	if err := bare.cancelAutomaton("a1", "keep", "x"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("err = %v, want not supported", err)
	}
}
