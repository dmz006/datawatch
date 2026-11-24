package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The daemon hands the server *autonomous.API. If it stops satisfying one of
// these, the routes silently fall back or answer "not supported".
var (
	_ outcomeCanceller      = (*autonomous.API)(nil)
	_ outcomeArchiver       = (*autonomous.API)(nil)
	_ cancelDecider         = (*autonomous.API)(nil)
	_ archiveDecider        = (*autonomous.API)(nil)
	_ cancelOutcomeDecider  = (*autonomous.API)(nil)
	_ archiveOutcomeDecider = (*autonomous.API)(nil)
)

// composeSpy records which cancel and archive form each route used.
type composeSpy struct{ finishedWorkSpy }

func (c *composeSpy) CancelWithOutcome(id string, in autonomous.OutcomeInput) error {
	c.cancelled = append(c.cancelled, id+"||"+in.Outcome)
	return nil
}
func (c *composeSpy) CancelDecidedWithOutcome(id, decision, reason string, in autonomous.OutcomeInput) error {
	c.cancelled = append(c.cancelled, id+"|"+decision+"|"+in.Outcome)
	return nil
}
func (c *composeSpy) ArchiveWithOutcome(id string, in autonomous.OutcomeInput) (any, error) {
	c.archived = append(c.archived, id+"||"+in.Outcome)
	return map[string]any{"id": id}, nil
}
func (c *composeSpy) ArchiveDecidedWithOutcome(id, decision, reason string, in autonomous.OutcomeInput) (any, error) {
	c.archived = append(c.archived, id+"|"+decision+"|"+in.Outcome)
	return map[string]any{"id": id}, nil
}

func TestRoutesCarryOutcomeAndDecisionAndActOnce(t *testing.T) {
	spy := &composeSpy{}
	s := &Server{autonomousMgr: spy}
	post := func(path, body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s %s: code = %d, body %s", path, body, rec.Code, rec.Body.String())
		}
	}
	both := `{"outcome":"dropped","decision":"drop","reason":"not wanted"}`
	post("/api/autonomous/prds/a1/cancel", "")
	post("/api/autonomous/prds/a2/cancel", `{"outcome":"held"}`)
	post("/api/autonomous/prds/a3/cancel", both)
	post("/api/autonomous/prds/b1/archive", "")
	post("/api/autonomous/prds/b2/archive", `{"outcome":"delivered"}`)
	post("/api/autonomous/prds/b3/archive", both)
	// One entry per request: no route cancels or archives twice.
	if got := strings.Join(spy.cancelled, ";"); got != "a1||;a2||held;a3|drop|dropped" {
		t.Errorf("cancelled = %s", got)
	}
	if got := strings.Join(spy.archived, ";"); got != "b1||;b2||delivered;b3|drop|dropped" {
		t.Errorf("archived = %s", got)
	}
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/a4/cancel", strings.NewReader("{not json")))
	if rec.Code != http.StatusBadRequest || len(spy.cancelled) != 3 {
		t.Errorf("bad body: code = %d, cancelled = %v", rec.Code, spy.cancelled)
	}
}
