// Held question config keys — Configuration Accessibility round trip for
// autonomous.questions.held_mode (off, shadow, on; shadow when absent) and
// autonomous.questions.held_states (the five held state ids when the list is
// absent, an explicit empty list when cleared). Both must come back out of
// GET /api/config as their effective values, an explicit value must survive a
// PUT and reach config.yaml, and a bad value is a 400 that applies and writes
// nothing.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// getHeldQuestionSurface decodes the questions block of GET /api/config and
// returns the held question values.
func getHeldQuestionSurface(t *testing.T, s *Server) (mode string, states []string) {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["questions"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks questions")
	}
	var q struct {
		HeldMode   *string   `json:"held_mode"`
		HeldStates *[]string `json:"held_states"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	if q.HeldMode == nil || q.HeldStates == nil {
		t.Fatalf("GET questions block lacks a held question key: %s", raw)
	}
	return *q.HeldMode, *q.HeldStates
}

func TestHeldQuestionConfigSurface_RESTGetDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	mode, states := getHeldQuestionSurface(t, s)
	if mode != "shadow" {
		t.Fatalf("GET default held_mode = %q, want shadow", mode)
	}
	want := []string{"blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"}
	if len(states) != len(want) {
		t.Fatalf("GET default held_states = %v, want the five ids", states)
	}
	for i, id := range want {
		if states[i] != id {
			t.Fatalf("GET default held_states = %v, want %v", states, want)
		}
	}
	q := s.cfg.Autonomous.Questions
	if q.HeldMode != "" || q.HeldStates != nil {
		t.Fatalf("stored values must be absent by default, got mode=%q states=%v", q.HeldMode, q.HeldStates)
	}
}

func TestHeldQuestionConfigSurface_RESTPutThenGetRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	// held_mode: each valid mode must land stored and come back out.
	for _, want := range []string{"on", "off", "shadow"} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.held_mode": want})
		if got := s.cfg.Autonomous.Questions.HeldMode; got != want {
			t.Fatalf("stored held_mode = %q, want %q", got, want)
		}
		if got, _ := getHeldQuestionSurface(t, s); got != want {
			t.Fatalf("GET after PUT held_mode=%q returned %q", want, got)
		}
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read config.yaml: %v", err)
		}
		yml := string(raw)
		if !strings.Contains(yml, "held_mode: "+want) &&
			!strings.Contains(yml, "held_mode: \""+want+"\"") &&
			!strings.Contains(yml, "held_mode: '"+want+"'") {
			t.Fatalf("config.yaml lacks held_mode %q:\n%s", want, raw)
		}
	}

	// held_states: an explicit list must survive and cover exactly those ids.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.held_states": []any{"review_held", "pending_insert"}})
	stored := s.cfg.Autonomous.Questions.HeldStates
	if len(stored) != 2 || stored[0] != "review_held" || stored[1] != "pending_insert" {
		t.Fatalf("stored held_states = %v, want the two ids", stored)
	}
	if _, states := getHeldQuestionSurface(t, s); len(states) != 2 || states[0] != "review_held" || states[1] != "pending_insert" {
		t.Fatalf("GET after PUT held_states returned the wrong list")
	}

	// held_states: an explicit empty list must read none, not all five.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.held_states": []any{}})
	stored = s.cfg.Autonomous.Questions.HeldStates
	if stored == nil || len(stored) != 0 {
		t.Fatalf("stored held_states = %v, want an explicit empty list", stored)
	}
	if _, states := getHeldQuestionSurface(t, s); len(states) != 0 {
		t.Fatalf("GET after PUT held_states=[] returned %v, want none", states)
	}
}

func TestHeldQuestionConfigSurface_BadModeReturns400(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	for _, bad := range []string{`"enforce"`, `"ON"`, `"shadowy"`, `true`, `1`} {
		body := `{"autonomous.questions.held_mode": ` + bad + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT held_mode=%s: want 400, got %d (%s)", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "held_mode") {
			t.Errorf("400 for %s should name the key; got %q", bad, rr.Body.String())
		}
		if s.cfg.Autonomous.Questions.HeldMode != "" {
			t.Fatalf("a rejected PUT (%s) changed the stored config: %q", bad, s.cfg.Autonomous.Questions.HeldMode)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Fatalf("a rejected PUT (%s) wrote config.yaml", bad)
		}
	}
}

func TestHeldQuestionConfigSurface_UnknownStateIDReturns400(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	body := `{"autonomous.questions.held_states": ["blocked", "bogus"]}`
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT held_states with an unknown id: want 400, got %d (%s)", rr.Code, rr.Body.String())
	}
	msg := rr.Body.String()
	if !strings.Contains(msg, `bogus`) {
		t.Errorf("400 should name the bad id; got %q", msg)
	}
	for _, id := range []string{"blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"} {
		if !strings.Contains(msg, id) {
			t.Errorf("400 should name the valid id %q; got %q", id, msg)
		}
	}
	if s.cfg.Autonomous.Questions.HeldStates != nil {
		t.Fatalf("a rejected PUT changed the held states: %v", s.cfg.Autonomous.Questions.HeldStates)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("a rejected PUT wrote config.yaml")
	}
}
