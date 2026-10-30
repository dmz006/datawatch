package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// supervisorWatchStatsSpy serves a fixed stats view and counts reads.
type supervisorWatchStatsSpy struct {
	fakeOrchAutonomous
	stats autonomous.SupervisorWatchStats
	calls int
}

func (s *supervisorWatchStatsSpy) SupervisorWatchStats() autonomous.SupervisorWatchStats {
	s.calls++
	return s.stats
}

func supervisorWatchStatsGET(s *Server, ctx context.Context) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/supervisor-watch/stats", nil).WithContext(ctx)
	s.handleSupervisorWatchStats(rec, req)
	return rec
}

func TestSupervisorWatchStatsRESTBody(t *testing.T) {
	spy := &supervisorWatchStatsSpy{stats: autonomous.SupervisorWatchStats{
		Mode: "shadow", LastPollAgeSeconds: 12, FindingsOpen: 3, FindingsWouldAlert: 1, FindingsAlerted: 1, FindingsResolved: 4, AgreedDays: 2,
	}}
	rec := supervisorWatchStatsGET(&Server{autonomousMgr: spy}, context.Background())
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]float64{
		"last_poll_age_seconds": 12, "findings_open": 3, "findings_would_alert": 1,
		"findings_alerted": 1, "findings_resolved": 4, "agreed_days": 2,
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if got["mode"] != "shadow" {
		t.Errorf("mode = %v", got["mode"])
	}
}

func TestSupervisorWatchStatsRESTGuards(t *testing.T) {
	spy := &supervisorWatchStatsSpy{}
	s := &Server{autonomousMgr: spy}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		s.handleSupervisorWatchStats(rec, httptest.NewRequest(method, "/api/autonomous/supervisor-watch/stats", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rec.Code)
		}
	}
	deny := context.WithValue(context.Background(), sessionCapsKey, []string{federation.CapHealthRead})
	if rec := supervisorWatchStatsGET(s, deny); rec.Code != http.StatusForbidden {
		t.Errorf("without the read capability: want 403, got %d", rec.Code)
	}
	if spy.calls != 0 {
		t.Errorf("refused requests reached the manager %d times", spy.calls)
	}
	if rec := supervisorWatchStatsGET(&Server{}, context.Background()); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("autonomous disabled: want 503, got %d", rec.Code)
	}
	// A manager without the stats accessor reads as unavailable.
	if rec := supervisorWatchStatsGET(&Server{autonomousMgr: &fakeOrchAutonomous{}}, context.Background()); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("manager without the accessor: want 503, got %d", rec.Code)
	}
}
