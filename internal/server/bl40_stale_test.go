// BL40 — stale-sessions REST tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestBL40_Stale_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/stale", nil)
	rr := httptest.NewRecorder()
	s.handleSessionsStale(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL40_Stale_FindsOldRunningSession(t *testing.T) {
	s := bl90Server(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "old", FullID: "testhost-old", Hostname: "testhost",
		State: session.StateRunning,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	})
	_ = s.manager.SaveSession(&session.Session{
		ID: "fresh", FullID: "testhost-fresh", Hostname: "testhost",
		State: session.StateRunning,
		UpdatedAt: time.Now(),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/stale?seconds=3600", nil)
	rr := httptest.NewRecorder()
	s.handleSessionsStale(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Count    int `json:"count"`
		Sessions []struct {
			FullID string `json:"full_id"`
		} `json:"sessions"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Count != 1 || len(got.Sessions) != 1 {
		t.Fatalf("expected 1 stale session, got %d (%+v)", got.Count, got.Sessions)
	}
	if got.Sessions[0].FullID != "testhost-old" {
		t.Errorf("wrong session: %+v", got.Sessions[0])
	}
}
