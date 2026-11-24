package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The real autonomous API wrapper — the exact value cmd/datawatch passes to
// the server — must serve the landing decision routes. Answering 501 "landing
// decisions are not supported by this daemon" means the wrapper dropped the
// landingDecider methods.
func TestRealAPIWrapperServesLandingDecisions(t *testing.T) {
	m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	s := &Server{autonomousMgr: autonomous.NewAPI(m)}
	for _, action := range []string{"keep", "drop"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/nope/landing/"+action, strings.NewReader(`{"reason":"r"}`))
		s.handleAutonomousPRDs(rr, req)
		if rr.Code == http.StatusNotImplemented {
			t.Fatalf("landing/%s answered 501: %s", action, rr.Body.String())
		}
		// An unknown id is a plain refusal, which proves the manager was reached.
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("landing/%s: status=%d body=%s, want 400 for an unknown Automaton", action, rr.Code, rr.Body.String())
		}
	}
}
