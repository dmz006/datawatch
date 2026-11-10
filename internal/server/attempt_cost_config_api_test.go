package server

// The three autonomous attempt-cost keys (autonomous.first_attempt,
// autonomous.first_attempt_escalate_after_failures,
// autonomous.retry_resume_max_context_tokens) through GET and PUT
// /api/config: a valid PUT round-trips to the GET surface and to the saved
// file; each refusal the shared config.ValidateAttemptCostSettings can make
// answers 400, messages that name the offending key, and leaves the stored
// value untouched; a patch that omits one of the keys keeps that key's
// stored value.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// attemptCostAPIServer returns a Server wired only for the config handlers:
// a no-op autonomous manager spy so PUT's config propagation reaches a fake,
// and a throwaway config path so config.Save/config.Load exercise a real
// file without touching operator config.
func attemptCostAPIServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	return &Server{
		cfg:           cfg,
		cfgPath:       t.TempDir() + "/attempt-cost-config.yaml",
		autonomousMgr: &fakeOrchAutonomous{},
	}
}

// putConfigRaw runs PUT /api/config with body and returns the status code
// and response body.
func putConfigRaw(t *testing.T, s *Server, body map[string]interface{}) (int, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(raw)))
	return rr.Code, rr.Body.String()
}

// getAutonomousBlock runs GET /api/config and returns its autonomous object.
func getAutonomousBlock(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var full map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	block, ok := full["autonomous"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET response has no autonomous object: %s", rr.Body.String())
	}
	return block
}

// storedValue reloads the saved config from disk and returns one of the
// three attempt-cost fields — the ground truth a refusal or partial patch
// must match.
func storedValue(t *testing.T, s *Server, which string) interface{} {
	t.Helper()
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	switch which {
	case "first_attempt":
		return loaded.Autonomous.FirstAttempt
	case "first_attempt_escalate_after_failures":
		return loaded.Autonomous.FirstAttemptEscalateAfterFailures
	case "retry_resume_max_context_tokens":
		return loaded.Autonomous.RetryResumeMaxContextTokens
	}
	t.Fatalf("unknown field %q", which)
	return nil
}

func asInt(v interface{}) int {
	f, _ := v.(float64)
	return int(f)
}

func TestAttemptCostConfigAPI_RoundTrip(t *testing.T) {
	s := attemptCostAPIServer(t, &config.Config{Git: config.GitConfig{ReleaseCIWindowMinutes: 60}})

	// Every key in one PUT, all valid.
	if code, body := putConfigRaw(t, s, map[string]interface{}{
		"autonomous.first_attempt": map[string]interface{}{
			"implementation": map[string]interface{}{"backend": "pi-prompt", "model": "qwen3.8:27b"},
			"note":           map[string]interface{}{},
		},
		"autonomous.first_attempt_escalate_after_failures": 3,
		"autonomous.retry_resume_max_context_tokens":       90000,
	}); code != http.StatusOK {
		t.Fatalf("valid PUT: want 200, got %d (%s)", code, body)
	}

	// The GET surface reports exactly what was written.
	block := getAutonomousBlock(t, s)
	fa, ok := block["first_attempt"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET first_attempt: want object, got %v", block["first_attempt"])
	}
	impl, _ := fa["implementation"].(map[string]interface{})
	if impl["backend"] != "pi-prompt" || impl["model"] != "qwen3.8:27b" {
		t.Fatalf("GET first_attempt.implementation: got %v", fa["implementation"])
	}
	if _, ok := fa["note"].(map[string]interface{}); !ok {
		t.Fatalf("GET first_attempt: empty-kind entry missing: %v", fa)
	}
	if got := asInt(block["first_attempt_escalate_after_failures"]); got != 3 {
		t.Fatalf("GET first_attempt_escalate_after_failures: got %v, want 3", block["first_attempt_escalate_after_failures"])
	}
	if got := asInt(block["retry_resume_max_context_tokens"]); got != 90000 {
		t.Fatalf("GET retry_resume_max_context_tokens: got %v, want 90000", block["retry_resume_max_context_tokens"])
	}

	// ...and the save persisted all three to disk.
	if m, _ := storedValue(t, s, "first_attempt").(map[string]config.FirstAttemptChoice); m["implementation"] != (config.FirstAttemptChoice{Backend: "pi-prompt", Model: "qwen3.8:27b"}) {
		t.Fatalf("persisted first_attempt.implementation: got %v", m)
	}
	if got := storedValue(t, s, "first_attempt_escalate_after_failures"); got != 3 {
		t.Fatalf("persisted first_attempt_escalate_after_failures: got %v, want 3", got)
	}
	if got := storedValue(t, s, "retry_resume_max_context_tokens"); got != 90000 {
		t.Fatalf("persisted retry_resume_max_context_tokens: got %v, want 90000", got)
	}
}

func TestAttemptCostConfigAPI_RefusalsLeaveStoredValueUnchanged(t *testing.T) {
	cases := []struct {
		name     string
		patch    map[string]interface{}
		check    string // which storedValue key holds wantKept after the refusal
		wantKept interface{}
		wantsIn  string // substring the 400 message must contain
	}{
		{
			name: "escalate-after-failures above the 20 ceiling",
			patch: map[string]interface{}{
				"autonomous.first_attempt_escalate_after_failures": 21,
			},
			check: "first_attempt_escalate_after_failures", wantKept: 2, wantsIn: "first_attempt_escalate_after_failures",
		},
		{
			name: "escalate-after-failures negative",
			patch: map[string]interface{}{
				"autonomous.first_attempt_escalate_after_failures": -5,
			},
			check: "first_attempt_escalate_after_failures", wantKept: 2, wantsIn: "first_attempt_escalate_after_failures",
		},
		{
			name: "retry-resume-tokens negative",
			patch: map[string]interface{}{
				"autonomous.retry_resume_max_context_tokens": -1,
			},
			check: "retry_resume_max_context_tokens", wantKept: 80000, wantsIn: "retry_resume_max_context_tokens",
		},
		{
			name: "retry-resume-tokens above the 2000000 ceiling",
			patch: map[string]interface{}{
				"autonomous.retry_resume_max_context_tokens": 2000001,
			},
			check: "retry_resume_max_context_tokens", wantKept: 80000, wantsIn: "retry_resume_max_context_tokens",
		},
		{
			name: "first_attempt unknown task kind",
			patch: map[string]interface{}{
				"autonomous.first_attempt": map[string]interface{}{
					"review": map[string]interface{}{"backend": "claude-code"},
				},
			},
			check: "first_attempt", wantKept: nil, wantsIn: "unknown task kind",
		},
		{
			name: "first_attempt backend that needs a model without one",
			patch: map[string]interface{}{
				"autonomous.first_attempt": map[string]interface{}{
					"docs": map[string]interface{}{"backend": "ollama"},
				},
			},
			check: "first_attempt", wantKept: nil, wantsIn: "needs a model",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := attemptCostAPIServer(t, &config.Config{Git: config.GitConfig{ReleaseCIWindowMinutes: 60}, Autonomous: config.AutonomousConfig{
				FirstAttemptEscalateAfterFailures: 2,
				RetryResumeMaxContextTokens:       80000,
			}})
			// Persist a valid baseline so "unchanged" is checked against
			// what a fresh load reads back from disk.
			if code, body := putConfigRaw(t, s, map[string]interface{}{
				"autonomous.first_attempt_escalate_after_failures": 2,
				"autonomous.retry_resume_max_context_tokens":       80000,
			}); code != http.StatusOK {
				t.Fatalf("baseline PUT: want 200, got %d (%s)", code, body)
			}

			code, body := putConfigRaw(t, s, tc.patch)
			if code != http.StatusBadRequest {
				t.Fatalf("refusal PUT: want 400, got %d (%s)", code, body)
			}
			if !strings.Contains(body, tc.wantsIn) {
				t.Fatalf("400 message %q must contain %q", body, tc.wantsIn)
			}
			// Neither the live config nor the saved file may have moved.
			if tc.check == "first_attempt" {
				if m, _ := storedValue(t, s, tc.check).(map[string]config.FirstAttemptChoice); len(m) != 0 {
					t.Fatalf("stored first_attempt after refusal: %v, want empty", m)
				}
			} else if got := storedValue(t, s, tc.check); got != tc.wantKept {
				t.Fatalf("stored value after refusal: got %v, want unchanged %v", got, tc.wantKept)
			}
			switch tc.check {
			case "first_attempt_escalate_after_failures":
				if got := s.cfg.Autonomous.FirstAttemptEscalateAfterFailures; got != 2 {
					t.Fatalf("live FirstAttemptEscalateAfterFailures after refusal: %d", got)
				}
			case "retry_resume_max_context_tokens":
				if got := s.cfg.Autonomous.RetryResumeMaxContextTokens; got != 80000 {
					t.Fatalf("live RetryResumeMaxContextTokens after refusal: %d", got)
				}
			default:
				if got := len(s.cfg.Autonomous.FirstAttempt); got != 0 {
					t.Fatalf("live FirstAttempt after refusal: %v", s.cfg.Autonomous.FirstAttempt)
				}
			}
		})
	}
}

func TestAttemptCostConfigAPI_AbsentKeyKeepsItsValue(t *testing.T) {
	s := attemptCostAPIServer(t, &config.Config{Git: config.GitConfig{ReleaseCIWindowMinutes: 60}})

	// Establish all three from one valid PUT.
	if code, body := putConfigRaw(t, s, map[string]interface{}{
		"autonomous.first_attempt": map[string]interface{}{
			"docs": map[string]interface{}{"backend": "pi-prompt", "model": "qwen3.8:27b"},
		},
		"autonomous.first_attempt_escalate_after_failures": 2,
		"autonomous.retry_resume_max_context_tokens":       80000,
	}); code != http.StatusOK {
		t.Fatalf("baseline PUT: want 200, got %d (%s)", code, body)
	}

	// Then a patch that touches ONLY retry_resume_max_context_tokens.
	if code, body := putConfigRaw(t, s, map[string]interface{}{
		"autonomous.retry_resume_max_context_tokens": 120000,
	}); code != http.StatusOK {
		t.Fatalf("partial PUT: want 200, got %d (%s)", code, body)
	}

	if got := storedValue(t, s, "retry_resume_max_context_tokens"); got != 120000 {
		t.Fatalf("touched key: got %v, want 120000", got)
	}
	if got := storedValue(t, s, "first_attempt_escalate_after_failures"); got != 2 {
		t.Fatalf("untouched first_attempt_escalate_after_failures moved: got %v, want 2", got)
	}
	if m, _ := storedValue(t, s, "first_attempt").(map[string]config.FirstAttemptChoice); m["docs"] != (config.FirstAttemptChoice{Backend: "pi-prompt", Model: "qwen3.8:27b"}) {
		t.Fatalf("untouched first_attempt.docs moved: got %v", m)
	}
}
