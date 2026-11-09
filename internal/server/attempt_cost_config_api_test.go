package server

// Attempt-cost settings surface — verify autonomous.first_attempt,
// autonomous.first_attempt_escalate_after_failures and autonomous.retry_resume_max_
// context_tokens round-trip through GET/PUT /api/config, that each of the
// shared config.ValidateAttemptCostSettings refusals answers 400 and leaves
// the stored value unchanged, and that a patch which omits a key keeps that
// key's existing value.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// newAttemptCostServer builds the minimal Server for GET/PUT /api/config:
// the live autonomous manager is a no-op spy (the PUT's SetConfig propagation
// lands there, never at the filesystem) and the config path is a throwaway
// temp file config.Save writes to / config.Load reads back from.
func newAttemptCostServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	return &Server{
		cfg:           cfg,
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: &fakeOrchAutonomous{},
	}
}

func bacPutConfig(t *testing.T, s *Server, body map[string]interface{}) (int, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	return rr.Code, rr.Body.String()
}

func getAutonomous(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var full map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode GET config: %v", err)
	}
	a, ok := full["autonomous"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET config: missing autonomous object (body=%s)", rr.Body.String())
	}
	return a
}

func roundTripStored(t *testing.T, s *Server, key string) interface{} {
	t.Helper()
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	switch key {
	case "autonomous.first_attempt":
		return loaded.Autonomous.FirstAttempt
	case "autonomous.first_attempt_escalate_after_failures":
		return loaded.Autonomous.FirstAttemptEscalateAfterFailures
	case "autonomous.retry_resume_max_context_tokens":
		return loaded.Autonomous.RetryResumeMaxContextTokens
	}
	t.Fatalf("unknown key %q", key)
	return nil
}

func firstAttemptStored(t *testing.T, s *Server) map[string]config.FirstAttemptChoice {
	t.Helper()
	v := roundTripStored(t, s, "autonomous.first_attempt")
	m, _ := v.(map[string]config.FirstAttemptChoice)
	return m
}

func TestAttemptCostConfigAPI_RoundTrip(t *testing.T) {
	s := newAttemptCostServer(t, &config.Config{})

	body := map[string]interface{}{
		"autonomous.first_attempt": map[string]interface{}{
			"implementation": map[string]interface{}{"backend": "pi-prompt", "model": "qwen3.8:27b"},
			"note":           map[string]interface{}{},
		},
		"autonomous.first_attempt_escalate_after_failures": 2,
		"autonomous.retry_resume_max_context_tokens":       80000,
	}
	if code, b := bacPutConfig(t, s, body); code != http.StatusOK {
		t.Fatalf("PUT valid attempt-cost settings: want 200, got %d body=%s", code, b)
	}

	a := getAutonomous(t, s)
	fa, ok := a["first_attempt"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET first_attempt: want object, got %v", a["first_attempt"])
	}
	impl, ok := fa["implementation"].(map[string]interface{})
	if !ok || impl["backend"] != "pi-prompt" || impl["model"] != "qwen3.8:27b" {
		t.Fatalf("GET first_attempt.implementation: got %v", fa["implementation"])
	}
	if _, ok := fa["note"].(map[string]interface{}); !ok {
		t.Fatalf("GET first_attempt.note: empty kind choice missing, got %v", fa)
	}
	if got := a["first_attempt_escalate_after_failures"]; got == nil || toIntAny(got) != 2 {
		t.Fatalf("GET first_attempt_escalate_after_failures: got %v, want 2", got)
	}
	if got := a["retry_resume_max_context_tokens"]; got == nil || toIntAny(got) != 80000 {
		t.Fatalf("GET retry_resume_max_context_tokens: got %v, want 80000", got)
	}

	// The save must have persisted all three — a fresh load out of the file
	// agrees with what the GET surface reported.
	implChoice := config.FirstAttemptChoice{Backend: "pi-prompt", Model: "qwen3.8:27b"}
	if stored := firstAttemptStored(t, s); stored["implementation"] != implChoice {
		t.Fatalf("persisted first_attempt.implementation: got %v", stored)
	}
	if got := roundTripStored(t, s, "autonomous.first_attempt_escalate_after_failures"); got != 2 {
		t.Fatalf("persisted first_attempt_escalate_after_failures: got %v, want 2", got)
	}
	if got := roundTripStored(t, s, "autonomous.retry_resume_max_context_tokens"); got != 80000 {
		t.Fatalf("persisted retry_resume_max_context_tokens: got %v, want 80000", got)
	}
}

func TestAttemptCostConfigAPI_RefusalsLeaveStoredValueUnchanged(t *testing.T) {
	cases := []struct {
		name          string
		initial       config.AutonomousConfig
		patch         map[string]interface{}
		key           string
		wantUnchanged int
		wantMsg       string
	}{
		{
			name:          "out-of-range escalate-after-failures (above max)",
			initial:       config.AutonomousConfig{FirstAttemptEscalateAfterFailures: 2, RetryResumeMaxContextTokens: 80000},
			patch:         map[string]interface{}{"autonomous.first_attempt_escalate_after_failures": 25},
			key:           "autonomous.first_attempt_escalate_after_failures",
			wantUnchanged: 2,
			wantMsg:       "autonomous.first_attempt_escalate_after_failures",
		},
		{
			name:          "out-of-range escalate-after-failures (negative)",
			initial:       config.AutonomousConfig{FirstAttemptEscalateAfterFailures: 2},
			patch:         map[string]interface{}{"autonomous.first_attempt_escalate_after_failures": -1},
			key:           "autonomous.first_attempt_escalate_after_failures",
			wantUnchanged: 2,
			wantMsg:       "autonomous.first_attempt_escalate_after_failures",
		},
		{
			name:          "out-of-range retry-resume-tokens (negative)",
			initial:       config.AutonomousConfig{RetryResumeMaxContextTokens: 80000},
			patch:         map[string]interface{}{"autonomous.retry_resume_max_context_tokens": -1},
			key:           "autonomous.retry_resume_max_context_tokens",
			wantUnchanged: 80000,
			wantMsg:       "autonomous.retry_resume_max_context_tokens",
		},
		{
			name:          "out-of-range retry-resume-tokens (above max)",
			initial:       config.AutonomousConfig{RetryResumeMaxContextTokens: 80000},
			patch:         map[string]interface{}{"autonomous.retry_resume_max_context_tokens": 2000001},
			key:           "autonomous.retry_resume_max_context_tokens",
			wantUnchanged: 80000,
			wantMsg:       "autonomous.retry_resume_max_context_tokens",
		},
		{
			name:    "unknown first_attempt kind",
			initial: config.AutonomousConfig{RetryResumeMaxContextTokens: 80000},
			patch: map[string]interface{}{"autonomous.first_attempt": map[string]interface{}{
				"review": map[string]interface{}{"backend": "claude-code"},
			}},
			key:     "autonomous.first_attempt",
			wantMsg: "unknown task kind",
		},
		{
			name:    "backend needing a model with none named",
			initial: config.AutonomousConfig{RetryResumeMaxContextTokens: 80000},
			patch: map[string]interface{}{"autonomous.first_attempt": map[string]interface{}{
				"docs": map[string]interface{}{"backend": "ollama"},
			}},
			key:     "autonomous.first_attempt",
			wantMsg: "autonomous.first_attempt.docs.model",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newAttemptCostServer(t, &config.Config{Autonomous: tc.initial})
			// Establish a persisted baseline so "unchanged" is measurable
			// against what was on disk before the refused PUT.
			if code, b := bacPutConfig(t, s, map[string]interface{}{
				"autonomous.first_attempt_escalate_after_failures": tc.initial.FirstAttemptEscalateAfterFailures,
				"autonomous.retry_resume_max_context_tokens":       tc.initial.RetryResumeMaxContextTokens,
			}); code != http.StatusOK {
				t.Fatalf("baseline PUT: want 200, got %d body=%s", code, b)
			}

			code, body := bacPutConfig(t, s, tc.patch)
			if code != http.StatusBadRequest {
				t.Fatalf("refusal PUT: want 400, got %d body=%s", code, body)
			}
			if !strings.Contains(body, tc.wantMsg) {
				t.Fatalf("refusal message %q does not name %q", body, tc.wantMsg)
			}

			switch tc.key {
			case "autonomous.first_attempt":
				if m := firstAttemptStored(t, s); len(m) != 0 {
					t.Fatalf("stored first_attempt after refusal: got %v, want empty", m)
				}
			default:
				if got := roundTripStored(t, s, tc.key); got != tc.wantUnchanged {
					t.Fatalf("stored %s after refusal: got %v, want %v (must be unchanged)", tc.key, got, tc.wantUnchanged)
				}
			}
		})
	}
}

func TestAttemptCostConfigAPI_AbsentKeyKeepsItsValue(t *testing.T) {
	s := newAttemptCostServer(t, &config.Config{})
	if code, b := bacPutConfig(t, s, map[string]interface{}{
		"autonomous.first_attempt":                         map[string]interface{}{"docs": map[string]interface{}{"backend": "pi-prompt", "model": "qwen3.8:27b"}},
		"autonomous.first_attempt_escalate_after_failures": 2,
		"autonomous.retry_resume_max_context_tokens":       80000,
	}); code != http.StatusOK {
		t.Fatalf("baseline PUT: want 200, got %d body=%s", code, b)
	}

	// A patch touching only ONE key must leave the other two exactly as stored.
	if code, b := bacPutConfig(t, s, map[string]interface{}{
		"autonomous.retry_resume_max_context_tokens": 120000,
	}); code != http.StatusOK {
		t.Fatalf("partial PUT: want 200, got %d body=%s", code, b)
	}

	if got := roundTripStored(t, s, "autonomous.first_attempt_escalate_after_failures"); got != 2 {
		t.Fatalf("escalate-after survived a patch that didn't touch it: got %v, want 2", got)
	}
	fa := firstAttemptStored(t, s)
	wantChoice := config.FirstAttemptChoice{Backend: "pi-prompt", Model: "qwen3.8:27b"}
	if fa["docs"] != wantChoice {
		t.Fatalf("first_attempt.docs survived a patch that didn't touch it: got %v", fa)
	}
	if got := roundTripStored(t, s, "autonomous.retry_resume_max_context_tokens"); got != 120000 {
		t.Fatalf("touched retry_resume_max_context_tokens: got %v, want 120000", got)
	}
}

// toIntAny reads a decoded-JSON number (float64) straight into an int for
// asserting GET /api/config values.
func toIntAny(v interface{}) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
