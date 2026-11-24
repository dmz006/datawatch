package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// priorityStallsSpy serves a fixed stall list and counts reads.
type priorityStallsSpy struct {
	fakeOrchAutonomous
	stalls []autonomous.PriorityStall
	calls  int
}

func (s *priorityStallsSpy) PriorityStalls() []autonomous.PriorityStall {
	s.calls++
	return s.stalls
}

func priorityStallsGET(s *Server, ctx context.Context) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/priority-stalls", nil).WithContext(ctx)
	s.handlePriorityStalls(rec, req)
	return rec
}

func TestPriorityStallsRESTBody(t *testing.T) {
	spy := &priorityStallsSpy{stalls: []autonomous.PriorityStall{{
		AutomatonID: "a1", Priority: 90, WaitingMinutes: 45, BlockerCode: "held",
		BlockerText: "the Automaton is on hold", Action: "release the hold",
	}}}
	rec := priorityStallsGET(&Server{autonomousMgr: spy}, context.Background())
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Stalls []autonomous.PriorityStall `json:"stalls"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Stalls) != 1 || got.Stalls[0] != spy.stalls[0] {
		t.Errorf("stalls = %+v", got.Stalls)
	}
}

func TestPriorityStallsRESTEmptyIsList(t *testing.T) {
	rec := priorityStallsGET(&Server{autonomousMgr: &priorityStallsSpy{}}, context.Background())
	if rec.Code != http.StatusOK || rec.Body.String() == "" {
		t.Fatalf("code %d body %q", rec.Code, rec.Body)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["stalls"]) != "[]" {
		t.Errorf("stalls = %s, want []", got["stalls"])
	}
}

func TestPriorityStallsRESTGuards(t *testing.T) {
	spy := &priorityStallsSpy{}
	s := &Server{autonomousMgr: spy}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handlePriorityStalls(rec, httptest.NewRequest(method, "/api/autonomous/priority-stalls", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	deny := context.WithValue(context.Background(), sessionCapsKey, []string{federation.CapHealthRead})
	if rec := priorityStallsGET(s, deny); rec.Code != http.StatusForbidden {
		t.Errorf("without the read capability: want 403, got %d", rec.Code)
	}
	if spy.calls != 0 {
		t.Errorf("refused requests reached the manager %d times", spy.calls)
	}
	if rec := priorityStallsGET(&Server{}, context.Background()); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no manager: want 503, got %d", rec.Code)
	}
}

func TestSupervisorWatchStallWakeConfigSurface(t *testing.T) {
	var c config.SupervisorWatchConfig
	if got := supervisorWatchSurface(c)["stall_wake"]; got != true {
		t.Errorf("unset stall_wake reads %v, want true", got)
	}
	off := false
	c.StallWake = &off
	if got := supervisorWatchSurface(c)["stall_wake"]; got != false {
		t.Errorf("explicit false reads %v", got)
	}
	key := supervisorWatchKeyPrefix + "stall_wake"
	if err := validateSupervisorWatchPatch(map[string]interface{}{key: "yes"}); err == nil {
		t.Error("a non-bool stall_wake must be refused")
	}
	if err := validateSupervisorWatchPatch(map[string]interface{}{key: false}); err != nil {
		t.Errorf("bool refused: %v", err)
	}
	var swc config.SupervisorWatchConfig
	applySupervisorWatchValue(&swc, "stall_wake", false)
	if swc.StallWake == nil || *swc.StallWake {
		t.Errorf("apply did not store false: %v", swc.StallWake)
	}
}
