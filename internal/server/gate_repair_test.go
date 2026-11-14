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

// gateRepairSpy stands in for the Manager: it answers the narrow read surface
// (GateRepairerStatus) and the two run entry points the gate-repair routes use.
type gateRepairSpy struct {
	fakeOrchAutonomous
	status    autonomous.GateRepairStatus
	singleOut autonomous.GateRepairOutcome
	passOuts  []autonomous.GateRepairOutcome
	single    int // how many RunGateRepair calls
	pass      int // how many RunGateRepairPass calls
	lastAuto  string
	lastTask  string
}

func (s *gateRepairSpy) GateRepairerStatus() autonomous.GateRepairStatus {
	return s.status
}

func (s *gateRepairSpy) RunGateRepair(_ context.Context, automatonID, taskID string) autonomous.GateRepairOutcome {
	s.single++
	s.lastAuto, s.lastTask = automatonID, taskID
	return s.singleOut
}

func (s *gateRepairSpy) RunGateRepairPass(_ context.Context) []autonomous.GateRepairOutcome {
	s.pass++
	return s.passOuts
}

func gateRepairServer() (*Server, *gateRepairSpy) {
	spy := &gateRepairSpy{
		status: autonomous.GateRepairStatus{
			Mode:   "report",
			Budget: []string{"aut/1/infra"},
			Records: []autonomous.RepairRecord{
				{Group: "gate", Subject: "task t1", Kind: "would", Found: "class: infra", Before: "before", After: "after"},
			},
		},
		singleOut: autonomous.GateRepairOutcome{
			Mode: "report", Class: "infra", Would: "re-run the failed gate",
			Kind: "would", Subject: "aut/1", Found: "class: infra",
		},
		passOuts: []autonomous.GateRepairOutcome{
			{Mode: "report", Class: "infra", Would: "re-run", Kind: "would", Subject: "aut/1"},
			{Mode: "report", Class: "code_or_test", Would: "reset the task", Kind: "would", Subject: "aut/2"},
		},
	}
	return &Server{autonomousMgr: spy}, spy
}

// --- GET /api/autonomous/gate-repair ---

func TestGateRepairREST_GetServed(t *testing.T) {
	s, _ := gateRepairServer()
	rec := httptest.NewRecorder()
	s.handleGateRepair(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/gate-repair", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var got autonomous.GateRepairStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "report" || len(got.Budget) != 1 || got.Budget[0] != "aut/1/infra" || len(got.Records) != 1 {
		t.Errorf("body = %+v", got)
	}
}

func TestGateRepairREST_GetReadOnly(t *testing.T) {
	s, spy := gateRepairServer()
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		s.handleGateRepair(rec, httptest.NewRequest(m, "/api/autonomous/gate-repair", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code = %d, want 405", m, rec.Code)
		}
	}
	if spy.single != 0 || spy.pass != 0 {
		t.Errorf("GET route must be read-only: single=%d pass=%d", spy.single, spy.pass)
	}
}

func TestGateRepairREST_GetCapabilityAndDisabled(t *testing.T) {
	s, _ := gateRepairServer()
	deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/gate-repair", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec := httptest.NewRecorder()
	s.handleGateRepair(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("want 403 naming the read capability, got %d %q", rec.Code, rec.Body.String())
	}
	allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/gate-repair", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleGateRepair(rec, allow)
	if rec.Code != http.StatusOK {
		t.Errorf("with read capability: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	(&Server{}).handleGateRepair(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled: %d", rec.Code)
	}
}

// --- POST /api/autonomous/gate-repair/run ---

func postGateRepairRun(s *Server, body, caps string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/gate-repair/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if caps != "" {
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, []string{caps}))
	}
	rec := httptest.NewRecorder()
	s.handleGateRepairRun(rec, req)
	return rec
}

func TestGateRepairREST_RunSingleGate(t *testing.T) {
	s, spy := gateRepairServer()
	rec := postGateRepairRun(s, `{"automaton_id":"aut","task_id":"t1"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%q", rec.Code, rec.Body.String())
	}
	var got gateRepairRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if spy.single != 1 || spy.pass != 0 {
		t.Errorf("expected one single-gate run, got single=%d pass=%d", spy.single, spy.pass)
	}
	if spy.lastAuto != "aut" || spy.lastTask != "t1" {
		t.Errorf("run targets = %q/%q", spy.lastAuto, spy.lastTask)
	}
	if len(got.Runs) != 1 || got.Runs[0].Class != "infra" || got.Runs[0].Would != "re-run the failed gate" {
		t.Errorf("runs = %+v", got.Runs)
	}
	if got.Status.Mode != "report" || len(got.Status.Records) != 1 {
		t.Errorf("status = %+v", got.Status)
	}
}

func TestGateRepairREST_RunPass(t *testing.T) {
	s, spy := gateRepairServer()
	rec := postGateRepairRun(s, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%q", rec.Code, rec.Body.String())
	}
	var got gateRepairRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if spy.pass != 1 || spy.single != 0 {
		t.Errorf("expected one pass, got pass=%d single=%d", spy.pass, spy.single)
	}
	if len(got.Runs) != 2 || got.Runs[0].Class != "infra" || got.Runs[1].Class != "code_or_test" {
		t.Errorf("runs = %+v", got.Runs)
	}
}

func TestGateRepairREST_RunPassEmptyBodyIsPass(t *testing.T) {
	s, spy := gateRepairServer()
	rec := postGateRepairRun(s, `{}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%q", rec.Code, rec.Body.String())
	}
	if spy.pass != 1 || spy.single != 0 {
		t.Errorf("an empty body must run a pass, got pass=%d single=%d", spy.pass, spy.single)
	}
}

func TestGateRepairREST_RunOneOfTwoRefused(t *testing.T) {
	s, spy := gateRepairServer()
	for _, body := range []string{
		`{"automaton_id":"aut"}`,
		`{"task_id":"t1"}`,
	} {
		rec := postGateRepairRun(s, body, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: want 400, got %d %q", body, rec.Code, rec.Body.String())
		}
	}
	if spy.single != 0 || spy.pass != 0 {
		t.Errorf("a refused run must not reach the manager: single=%d pass=%d", spy.single, spy.pass)
	}
}

func TestGateRepairREST_RunMalformedBody(t *testing.T) {
	s, spy := gateRepairServer()
	rec := postGateRepairRun(s, `{"automaton_id": `, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for a malformed body, got %d %q", rec.Code, rec.Body.String())
	}
	if spy.single != 0 || spy.pass != 0 {
		t.Errorf("a malformed body must not reach the manager: single=%d pass=%d", spy.single, spy.pass)
	}
}

func TestGateRepairREST_RunReadOnlyMethod(t *testing.T) {
	s, spy := gateRepairServer()
	rec := httptest.NewRecorder()
	s.handleGateRepairRun(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/gate-repair/run", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET on the run route: want 405, got %d", rec.Code)
	}
	if spy.single != 0 || spy.pass != 0 {
		t.Errorf("a refused method must not reach the manager: single=%d pass=%d", spy.single, spy.pass)
	}
}

func TestGateRepairREST_RunCapabilityAndDisabled(t *testing.T) {
	s, _ := gateRepairServer()
	// A reader-cap token cannot run the repair.
	rec := postGateRepairRun(s, `{}`, federation.CapAutonomousRead)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRun) {
		t.Errorf("want 403 naming the run capability, got %d %q", rec.Code, rec.Body.String())
	}
	// A run-cap token can.
	rec = postGateRepairRun(s, `{"automaton_id":"aut","task_id":"t1"}`, federation.CapAutonomousRun)
	if rec.Code != http.StatusOK {
		t.Errorf("with run capability: %d %q", rec.Code, rec.Body.String())
	}
	// Disabled autonomous: 503.
	rec = httptest.NewRecorder()
	(&Server{}).handleGateRepairRun(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"automaton_id":"a","task_id":"t"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled: %d", rec.Code)
	}
}
