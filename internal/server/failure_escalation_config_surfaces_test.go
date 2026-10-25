// autonomous.failure_escalation — Configuration Accessibility round-trip for the
// global default, on both REST config surfaces. GET /api/config shows
// {enabled, max_failures, steps}; PUT /api/config persists the block to
// config.yaml and reaches the live manager without a restart; steps accepts a
// JSON array or the compact text backend:model[@attempts],...; and a bad value
// refuses the whole PUT with a 400 that names the key, changing nothing.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// getFailureEscalationDisplay decodes the failure_escalation block of GET
// /api/config.
func getFailureEscalationDisplay(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["failure_escalation"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks failure_escalation")
	}
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("decode failure_escalation: %v (%s)", err, raw)
	}
	return block
}

// liveFailureEscalation returns the manager's live global default.
func liveFailureEscalation(mgr *autonomous.Manager) autonomous.FailureEscalationPolicy {
	return mgr.Config().FailureEscalation
}

// putRaw sends a PUT /api/config body and returns the recorder, without asserting
// the status, so error cases can be checked.
func putRaw(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func TestFailureEscalationConfigGetDefault(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	block := getFailureEscalationDisplay(t, s)

	if got := string(block["enabled"]); got != "true" {
		t.Errorf("enabled = %s, want true", got)
	}
	if got := string(block["max_failures"]); got != "3" {
		t.Errorf("max_failures = %s, want 3", got)
	}
	if got := string(block["steps"]); got != "[]" {
		t.Errorf("steps = %s, want an empty array (never null)", got)
	}
	if live := liveFailureEscalation(mgr); live.Enabled == nil || !*live.Enabled || live.MaxFailures != 3 {
		t.Errorf("live manager default = %+v, want enabled and 3", live)
	}
}

func TestFailureEscalationConfigPutRoundTripPersistsAndPropagates(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.failure_escalation.enabled":      false,
		"autonomous.failure_escalation.max_failures": 4,
	})

	if s.cfg.Autonomous.FailureEscalation.Enabled || s.cfg.Autonomous.FailureEscalation.MaxFailures != 4 {
		t.Fatalf("s.cfg after PUT = %+v", s.cfg.Autonomous.FailureEscalation)
	}
	block := getFailureEscalationDisplay(t, s)
	if string(block["enabled"]) != "false" || string(block["max_failures"]) != "4" {
		t.Fatalf("GET after PUT = %s / %s", block["enabled"], block["max_failures"])
	}
	live := liveFailureEscalation(mgr)
	if live.Enabled == nil || *live.Enabled || live.MaxFailures != 4 {
		t.Fatalf("live manager after PUT = %+v, want enabled=false max_failures=4", live)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, want := range []string{"failure_escalation:", "enabled: false", "max_failures: 4"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, raw)
		}
	}
	// The saved file must load again: the loader rejects unknown keys in this
	// block, so a key the writer emits but the reader does not know shows here.
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.yaml written by PUT does not load: %v", err)
	}
	if reloaded.Autonomous.FailureEscalation.Enabled || reloaded.Autonomous.FailureEscalation.MaxFailures != 4 {
		t.Errorf("reloaded failure_escalation = %+v", reloaded.Autonomous.FailureEscalation)
	}
}

// TestFailureEscalationConfigAutonomousReadMatches: GET /api/autonomous/config
// (the manager's own config read) carries the same block as GET /api/config.
func TestFailureEscalationConfigAutonomousReadMatches(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.failure_escalation.max_failures": 5,
		"autonomous.failure_escalation.steps":        "claude-code:claude-opus-5-5@2",
	})
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/autonomous/config: %d %s", rr.Code, rr.Body.String())
	}
	var got struct {
		FailureEscalation struct {
			Enabled     *bool `json:"enabled"`
			MaxFailures int   `json:"max_failures"`
			Steps       []struct {
				Backend  string `json:"backend"`
				Model    string `json:"model"`
				Attempts int    `json:"attempts"`
			} `json:"steps"`
		} `json:"failure_escalation"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	fe := got.FailureEscalation
	if fe.Enabled == nil || !*fe.Enabled || fe.MaxFailures != 5 {
		t.Fatalf("autonomous config failure_escalation = %+v", fe)
	}
	if len(fe.Steps) != 1 || fe.Steps[0].Backend != "claude-code" || fe.Steps[0].Attempts != 2 {
		t.Fatalf("autonomous config steps = %+v", fe.Steps)
	}
}

func TestFailureEscalationConfigStepsCompactText(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.failure_escalation.steps": "pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5,-",
	})

	steps := s.cfg.Autonomous.FailureEscalation.Steps
	if len(steps) != 3 {
		t.Fatalf("s.cfg steps = %+v, want 3", steps)
	}
	if steps[0].Backend != "pi-prompt" || steps[0].Model != "ollama/qwen3.8:27b" || steps[0].Attempts != 2 {
		t.Errorf("step 0 = %+v", steps[0])
	}
	if steps[2].Backend != "" {
		t.Errorf("step 2 (-) should use the normal worker, got backend %q", steps[2].Backend)
	}
	if live := liveFailureEscalation(mgr); len(live.Steps) != 3 || live.Steps[1].Model != "claude-opus-5-5" {
		t.Errorf("live steps = %+v", live.Steps)
	}
	var got []map[string]any
	if err := json.Unmarshal(getFailureEscalationDisplay(t, s)["steps"], &got); err != nil || len(got) != 3 {
		t.Errorf("GET steps = %v (err %v), want 3 entries", got, err)
	}
}

func TestFailureEscalationConfigStepsJSONArray(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.failure_escalation.steps": []any{
			map[string]any{"backend": "pi-prompt", "model": "ollama/qwen3.8:27b", "attempts": 2},
			map[string]any{"backend": "claude-code", "model": "claude-opus-5-5", "effort": "high"},
		},
	})

	steps := s.cfg.Autonomous.FailureEscalation.Steps
	if len(steps) != 2 || steps[1].Effort != "high" || steps[0].Attempts != 2 {
		t.Fatalf("s.cfg steps = %+v", steps)
	}
	if live := liveFailureEscalation(mgr); len(live.Steps) != 2 || live.Steps[1].Effort != autonomous.EffortHigh {
		t.Fatalf("live steps = %+v", live.Steps)
	}

	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.failure_escalation.steps": ""})
	if n := len(s.cfg.Autonomous.FailureEscalation.Steps); n != 0 {
		t.Fatalf("empty steps text should clear the ladder, %d left", n)
	}
}

// TestFailureEscalationConfigInvalidValuesRefused: each bad value is a 400 that
// names its key and changes neither the stored config nor the live manager.
func TestFailureEscalationConfigInvalidValuesRefused(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		keyWant string
	}{
		{"max_failures zero", map[string]any{"autonomous.failure_escalation.max_failures": 0}, "max_failures"},
		{"max_failures negative", map[string]any{"autonomous.failure_escalation.max_failures": -2}, "max_failures"},
		{"max_failures text", map[string]any{"autonomous.failure_escalation.max_failures": "many"}, "max_failures"},
		{"enabled not boolean", map[string]any{"autonomous.failure_escalation.enabled": "maybe"}, "enabled"},
		{"steps bad effort", map[string]any{"autonomous.failure_escalation.steps": []any{
			map[string]any{"backend": "claude-code", "effort": "bogus"},
		}}, "effort"},
		{"steps unknown key", map[string]any{"autonomous.failure_escalation.steps": []any{
			map[string]any{"backend": "claude-code", "temperature": 1},
		}}, "steps"},
		{"steps text without colon", map[string]any{"autonomous.failure_escalation.steps": "claude-code"}, "steps"},
		{"steps text bad attempts", map[string]any{"autonomous.failure_escalation.steps": "claude-code:x@0"}, "steps"},
		{"steps wrong type", map[string]any{"autonomous.failure_escalation.steps": 7}, "steps"},
		{"too many steps", map[string]any{"autonomous.failure_escalation.steps": strings.Repeat("claude-code:m,", 8) + "claude-code:m"}, "steps"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			// A valid companion key in the same PUT must not be applied either
			// (a bad key replaces it when both name max_failures).
			body := map[string]any{"autonomous.failure_escalation.max_failures": 5}
			for k, v := range tc.body {
				body[k] = v
			}
			rr := putRaw(t, s, body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT %v: want 400, got %d (%s)", body, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.keyWant) {
				t.Errorf("400 message should name %q, got %q", tc.keyWant, rr.Body.String())
			}
			if got := s.cfg.Autonomous.FailureEscalation; !got.Enabled || got.MaxFailures != 3 || len(got.Steps) != 0 {
				t.Errorf("stored config changed by a refused PUT: %+v", got)
			}
			if live := liveFailureEscalation(mgr); live.Enabled == nil || !*live.Enabled || live.MaxFailures != 3 {
				t.Errorf("live manager changed by a refused PUT: %+v", live)
			}
		})
	}
}
