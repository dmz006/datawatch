// The PRD cancel reply reports killed_sessions (sessions a kill was
// attempted on) and killed_live_sessions (how many of those were not already
// ended when the cancel began).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/dmz006/datawatch/internal/autonomous"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// saveTestSessions records one session per (id, state) pair in the server's
// session manager without starting anything.
func saveTestSessions(t *testing.T, s *Server, states map[string]session.State) {
	t.Helper()
	for id, st := range states {
		if err := s.manager.SaveSession(&session.Session{ID: id, FullID: "h-" + id, State: st}); err != nil {
			t.Fatalf("SaveSession %s: %v", id, err)
		}
	}
}

func assertLive(t *testing.T, m map[string]any, wantKilled, wantLive float64) {
	t.Helper()
	if got, ok := m["killed_sessions"].(float64); !ok || got != wantKilled {
		t.Errorf("killed_sessions = %#v, want %v", m["killed_sessions"], wantKilled)
	}
	if got, ok := m["killed_live_sessions"].(float64); !ok || got != wantLive {
		t.Errorf("killed_live_sessions = %#v, want %v", m["killed_live_sessions"], wantLive)
	}
}

func TestAutonomousPRDCancel_CountsLiveSessions(t *testing.T) {
	s, al := newCancelTestServer(t, []string{"aa01", "aa02", "aa03"})
	saveTestSessions(t, s, map[string]session.State{
		"aa01": session.StateComplete, "aa02": session.StateKilled, "aa03": session.StateRunning,
	})
	body := doPRDDelete(t, s, "/api/autonomous/prds/prd-l1")
	assertLive(t, body, 3, 1)
	assertLive(t, lastAuditDetails(t, al), 3, 1)
}

func TestAutonomousPRDHardDelete_CountsLiveSessions(t *testing.T) {
	s, _ := newCancelTestServer(t, []string{"bb01", "bb02"})
	saveTestSessions(t, s, map[string]session.State{
		"bb01": session.StateWaitingInput, "bb02": session.StateFailed,
	})
	assertLive(t, doPRDDelete(t, s, "/api/autonomous/prds/prd-l2?hard=true"), 2, 1)
}

func TestAutonomousPRDCancel_OnlyEndedSessionsNoneLive(t *testing.T) {
	s, _ := newCancelTestServer(t, []string{"cc01", "cc02"})
	saveTestSessions(t, s, map[string]session.State{
		"cc01": session.StateComplete, "cc02": session.StateFailed,
	})
	assertLive(t, doPRDDelete(t, s, "/api/autonomous/prds/prd-l3"), 2, 0)
}

func TestAutonomousPRDCancel_NoSessionsBothZero(t *testing.T) {
	s, _ := newCancelTestServer(t, nil)
	assertLive(t, doPRDDelete(t, s, "/api/autonomous/prds/prd-l4"), 0, 0)
}

func TestAutonomousPRDCancel_CompactViewCarriesLiveCount(t *testing.T) {
	s, _ := newCancelTestServer(t, []string{"dd01", "dd02", "dd03"})
	saveTestSessions(t, s, map[string]session.State{
		"dd01": session.StateComplete, "dd02": session.StateKilled, "dd03": session.StateRunning,
	})
	body := doPRDDelete(t, s, "/api/autonomous/prds/prd-l5?view=compact")
	assertLive(t, body, 3, 1)
}

func TestWriteAutonomousCancelResult_CompactCarriesLiveCount(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/autonomous/prds/p?view=compact", nil)
	s.writeAutonomousCancelResult(rr, req, &autonomous.PRD{ID: "p", Status: autonomous.PRDCancelled}, 3, 1, map[string]any{"full": true})
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, full := body["full"]; full {
		t.Fatalf("expected the compact view, got the full reply: %#v", body)
	}
	assertLive(t, body, 3, 1)
}
