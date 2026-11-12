// Decision gate config keys — Configuration Accessibility round trip for
// autonomous.questions.decision_gate (bool, on when absent) and
// autonomous.questions.decision_gate_mode (shadow or on, shadow when absent).
// Both must come back out of GET /api/config as their effective values, an
// explicit value must survive a PUT and reach config.yaml, and a bad value is
// a 400 that applies and writes nothing.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// getDecisionGateSurface decodes the questions block of GET /api/config and
// returns the two decision gate values.
func getDecisionGateSurface(t *testing.T, s *Server) (gate bool, mode string) {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["questions"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks questions")
	}
	var q struct {
		DecisionGate     *bool   `json:"decision_gate"`
		DecisionGateMode *string `json:"decision_gate_mode"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	if q.DecisionGate == nil || q.DecisionGateMode == nil {
		t.Fatalf("GET questions block lacks a decision gate key: %s", raw)
	}
	return *q.DecisionGate, *q.DecisionGateMode
}

func TestDecisionGateConfigSurface_RESTGetDefaults(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	gate, mode := getDecisionGateSurface(t, s)
	if !gate || mode != "shadow" {
		t.Fatalf("GET defaults: decision_gate=%v decision_gate_mode=%q, want true and shadow", gate, mode)
	}
	q := s.cfg.Autonomous.Questions
	if q.DecisionGate != nil || q.DecisionGateMode != "" {
		t.Fatalf("stored values must be absent by default, got %+v", q)
	}
}

func TestDecisionGateConfigSurface_RESTPutThenGetRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	// decision_gate: an explicit false must survive, and so must true after it.
	for _, want := range []bool{false, true} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.decision_gate": want})
		if b := s.cfg.Autonomous.Questions.DecisionGate; b == nil || *b != want {
			t.Fatalf("stored decision_gate = %v, want explicit %v", b, want)
		}
		if gate, _ := getDecisionGateSurface(t, s); gate != want {
			t.Fatalf("GET after PUT decision_gate=%v returned %v", want, gate)
		}
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read config.yaml: %v", err)
		}
		if frag := "decision_gate: " + map[bool]string{true: "true", false: "false"}[want]; !strings.Contains(string(raw), frag) {
			t.Fatalf("config.yaml lacks %q:\n%s", frag, raw)
		}
	}

	// decision_gate_mode: on, then back to shadow.
	for _, want := range []string{"on", "shadow"} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.decision_gate_mode": want})
		if got := s.cfg.Autonomous.Questions.DecisionGateMode; got != want {
			t.Fatalf("stored decision_gate_mode = %q, want %q", got, want)
		}
		if _, mode := getDecisionGateSurface(t, s); mode != want {
			t.Fatalf("GET after PUT decision_gate_mode=%q returned %q", want, mode)
		}
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read config.yaml: %v", err)
		}
		// YAML quotes "on" so it is not read back as a boolean.
		if yml := string(raw); !strings.Contains(yml, "decision_gate_mode: "+want) &&
			!strings.Contains(yml, `decision_gate_mode: "`+want+`"`) {
			t.Fatalf("config.yaml lacks decision_gate_mode %q:\n%s", want, raw)
		}
	}
}

func TestDecisionGateConfigSurface_BadModeReturns400(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	for _, bad := range []string{`"bogus"`, `"ON"`, `"Shadow"`, `"off"`, `"true"`, `true`, `1`, `null`} {
		// Sent beside a valid decision_gate=false: the whole PUT is refused,
		// so that valid key must not land either.
		body := `{"autonomous.questions.decision_gate": false, "autonomous.questions.decision_gate_mode": ` + bad + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT decision_gate_mode=%s: want 400, got %d (%s)", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "decision_gate_mode") {
			t.Errorf("400 for %s should name the key; got %q", bad, rr.Body.String())
		}
		q := s.cfg.Autonomous.Questions
		if q.DecisionGate != nil || q.DecisionGateMode != "" {
			t.Fatalf("a rejected PUT (%s) changed the stored config: %+v", bad, q)
		}
		if gate, mode := getDecisionGateSurface(t, s); !gate || mode != "shadow" {
			t.Fatalf("GET after rejected PUT %s: gate=%v mode=%q, want true and shadow", bad, gate, mode)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Fatalf("a rejected PUT (%s) wrote config.yaml", bad)
		}
	}
}

func TestDecisionGateConfigSurface_NonBooleanGateRejected(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	for _, bad := range []string{`"yes"`, `"true"`, `1`, `null`} {
		body := `{"autonomous.questions.decision_gate": ` + bad + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT decision_gate=%s: want 400, got %d (%s)", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "boolean") {
			t.Errorf("400 for %s should say a boolean is required; got %q", bad, rr.Body.String())
		}
		if s.cfg.Autonomous.Questions.DecisionGate != nil {
			t.Fatalf("a rejected value was stored: %v", *s.cfg.Autonomous.Questions.DecisionGate)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Fatalf("a rejected PUT (%s) wrote config.yaml", bad)
		}
	}
}
