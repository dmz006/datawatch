package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// guardReportAPI embeds the autonomous API interface (nil) and overrides only
// the restart report, which is all the restart handler reads.
type guardReportAPI struct {
	AutonomousAPI
	rep autonomous.RestartInterruption
}

func (g guardReportAPI) RestartInterruptions() autonomous.RestartInterruption { return g.rep }

func needsForceReport() autonomous.RestartInterruption {
	return autonomous.RestartInterruption{
		Policy:           "immediate",
		PlanningAutomata: []autonomous.PlanningAutomaton{{ID: "auto-1"}},
		LiveSessions:     []string{"sess-1"},
		PlanningCnt:      1,
		LiveSessionCnt:   1,
		NeedsForce:       true,
	}
}

func TestRestartGuard409WithoutForce(t *testing.T) {
	s := &Server{hub: NewHub(), autonomousMgr: guardReportAPI{rep: needsForceReport()}}
	called := make(chan bool, 1)
	s.restartForceFn = func(force bool) { called <- force }

	w := httptest.NewRecorder()
	s.handleRestart(w, httptest.NewRequest(http.MethodPost, "/api/restart", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var body struct {
		NeedsForce bool   `json:"needs_force"`
		HowToForce string `json:"how_to_force"`
		Interrupts struct {
			PlanningAutomata []autonomous.PlanningAutomaton `json:"planning_automata"`
		} `json:"interrupts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	if !body.NeedsForce || body.HowToForce == "" || len(body.Interrupts.PlanningAutomata) != 1 {
		t.Fatalf("409 body missing report or force hint: %s", w.Body.String())
	}
	select {
	case <-called:
		t.Fatalf("restart ran for a refused request")
	case <-time.After(800 * time.Millisecond):
	}
}

func TestRestartGuardProceedsWithForce(t *testing.T) {
	s := &Server{hub: NewHub(), autonomousMgr: guardReportAPI{rep: needsForceReport()}}
	called := make(chan bool, 1)
	s.restartForceFn = func(force bool) { called <- force }

	w := httptest.NewRecorder()
	s.handleRestart(w, httptest.NewRequest(http.MethodPost, "/api/restart?force=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	select {
	case force := <-called:
		if !force {
			t.Fatalf("restart called with force=false; want true")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("restart not called with force")
	}
}

func TestRestartGuardProceedsWithNoForceWhenNotNeeded(t *testing.T) {
	rep := autonomous.RestartInterruption{Policy: "immediate", NeedsForce: false}
	s := &Server{hub: NewHub(), autonomousMgr: guardReportAPI{rep: rep}}
	called := make(chan bool, 1)
	s.restartForceFn = func(force bool) { called <- force }

	w := httptest.NewRecorder()
	s.handleRestart(w, httptest.NewRequest(http.MethodPost, "/api/restart", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	select {
	case force := <-called:
		if force {
			t.Fatalf("restart called with force=true; want false")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("restart not called when nothing needs force")
	}
}
