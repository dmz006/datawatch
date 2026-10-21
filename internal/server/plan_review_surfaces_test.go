// 2026-10-17 — plan review gate + smoke verification: Configuration
// Accessibility Rule surfaces for the five operator-configurable fields
// (autonomous.plan_review_{backend,model,mode},
// autonomous.smoke_verification_{backend,model}) and the
// POST /api/autonomous/prds/{id}/plan-review re-run endpoint, plus the
// 400 guard for unknown plan_review_mode values (previously a typo
// would normalize to "off" and silently disable the gate).

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestApplyConfigPatch_PlanReviewAndSmokeVerification — the five key
// strings round-trip through applyConfigPatch (the same precedent as
// B119's execution_backend single-field checks).
func TestApplyConfigPatch_PlanReviewAndSmokeVerification(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.plan_review_backend":        "anthropic",
		"autonomous.plan_review_model":          "claude-sonnet",
		"autonomous.plan_review_mode":           "warn",
		"autonomous.smoke_verification_backend": "ollama-reviewer",
		"autonomous.smoke_verification_model":   "llama3.1",
	})
	if got := cfg.Autonomous.PlanReviewBackend; got != "anthropic" {
		t.Fatalf("plan_review_backend: got %q, want anthropic", got)
	}
	if got := cfg.Autonomous.PlanReviewModel; got != "claude-sonnet" {
		t.Fatalf("plan_review_model: got %q, want claude-sonnet", got)
	}
	if got := cfg.Autonomous.PlanReviewMode; got != "warn" {
		t.Fatalf("plan_review_mode: got %q, want warn", got)
	}
	if got := cfg.Autonomous.SmokeVerificationBackend; got != "ollama-reviewer" {
		t.Fatalf("smoke_verification_backend: got %q, want ollama-reviewer", got)
	}
	if got := cfg.Autonomous.SmokeVerificationModel; got != "llama3.1" {
		t.Fatalf("smoke_verification_model: got %q, want llama3.1", got)
	}

	// Explicit clear (empty string) must round-trip, not be elided.
	clear := map[string]interface{}{
		"autonomous.plan_review_backend":        "",
		"autonomous.plan_review_mode":           "off",
		"autonomous.smoke_verification_backend": "",
		"autonomous.smoke_verification_model":   "",
	}
	applyConfigPatch(cfg, clear)
	if cfg.Autonomous.PlanReviewBackend != "" || cfg.Autonomous.SmokeVerificationBackend != "" ||
		cfg.Autonomous.SmokeVerificationModel != "" {
		t.Fatalf("explicit clear must take effect, got %+v", cfg.Autonomous)
	}
	if cfg.Autonomous.PlanReviewMode != "off" {
		t.Fatalf("plan_review_mode: got %q, want off", cfg.Autonomous.PlanReviewMode)
	}
}

// TestConfigRoundTrip_PlanReviewGate — PUT /api/config then GET /api/config
// must show all five keys, and the live manager must receive them
// (BL369 propagation loop), following the block_on_file_drift precedent.
func TestConfigRoundTrip_PlanReviewGate(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}

	put := func(vals map[string]any) {
		t.Helper()
		body, _ := json.Marshal(vals)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", vals, rr.Code, rr.Body.String())
		}
	}

	keys := []string{
		"plan_review_backend", "plan_review_model", "plan_review_mode",
		"smoke_verification_backend", "smoke_verification_model",
	}
	for _, k := range keys {
		if _, ok := gotAutonomousField(t, s, k); !ok {
			t.Fatalf("GET surface must expose autonomous.%s", k)
		}
	}

	put(map[string]any{
		"autonomous.plan_review_backend":        "anthropic",
		"autonomous.plan_review_model":          "claude-sonnet",
		"autonomous.plan_review_mode":           "block",
		"autonomous.smoke_verification_backend": "ollama-reviewer",
		"autonomous.smoke_verification_model":   "llama3.1",
	})

	checks := []struct{ key, want string }{
		{"plan_review_backend", `"anthropic"`},
		{"plan_review_model", `"claude-sonnet"`},
		{"plan_review_mode", `"block"`},
		{"smoke_verification_backend", `"ollama-reviewer"`},
		{"smoke_verification_model", `"llama3.1"`},
	}
	for _, c := range checks {
		raw, _ := gotAutonomousField(t, s, c.key)
		if string(raw) != c.want {
			t.Errorf("GET autonomous.%s = %s, want %s", c.key, raw, c.want)
		}
	}

	// The live manager must have received the same fields.
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	for _, c := range checks {
		raw, ok := fwd[c.key]
		if !ok || string(raw) != c.want {
			t.Errorf("propagated payload %s = present %v / %s, want %s (raw: %s)",
				c.key, ok, string(raw), c.want, spy.lastSetConfig)
		}
	}
}

// gotAutonomousField is a small helper so TestConfigRoundTrip_PlanReviewGate
// can assert GET-surface presence before the PUT it performs.
func gotAutonomousField(t *testing.T, s *Server, key string) (json.RawMessage, bool) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	raw, ok := got.Autonomous[key]
	return raw, ok
}

// Unknown plan_review_mode values must be rejected with 400 instead of
// silently normalizing to "off" (which would disable the gate).
func TestHandlePutConfig_InvalidPlanReviewModeRejected(t *testing.T) {
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: &autonomousConfigPropagationSpy{},
	}
	for _, mode := range []string{"banana", "BLOCKED", "Warn!"} {
		body, _ := json.Marshal(map[string]any{"autonomous.plan_review_mode": mode})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("plan_review_mode=%q: want 400, got %d body=%s", mode, rr.Code, rr.Body.String())
		}
		if cfg := s.cfg; cfg.Autonomous.PlanReviewMode != "" {
			t.Errorf("rejected write must not persist, stored %q", cfg.Autonomous.PlanReviewMode)
		}
	}
	// Valid modes still pass.
	for _, mode := range []string{"off", "warn", "block", "BLOCK"} {
		body, _ := json.Marshal(map[string]any{"autonomous.plan_review_mode": mode})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Errorf("plan_review_mode=%q: want 200, got %d body=%s", mode, rr.Code, rr.Body.String())
		}
		if s.cfg.Autonomous.PlanReviewMode != mode {
			t.Errorf("plan_review_mode=%q: stored %q", mode, s.cfg.Autonomous.PlanReviewMode)
		}
	}
}

// planReviewFake implements the optional plan-review surface on top of the
// standard config-propagation spy, recording who asked for a re-run.
type planReviewFake struct {
	autonomousConfigPropagationSpy
	planReviewCalls int
	lastActor       string
}

func (f *planReviewFake) ReviewPlan(id, actor string) (any, error) {
	f.planReviewCalls++
	f.lastActor = actor
	return map[string]any{
		"id":      id,
		"status":  "needs_review",
		"verdict": "approved",
	}, nil
}

// TestAutonomousPRDPlanReviewEndpoint — the re-run endpoint must call the
// manager's ReviewPlan surface with the operator's actor and surface the
// verdict back to the caller.
func TestAutonomousPRDPlanReviewEndpoint(t *testing.T) {
	f := &planReviewFake{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: f,
	}

	body, _ := json.Marshal(map[string]string{"actor": "alice"})
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/p1/plan-review", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("plan-review: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if f.planReviewCalls != 1 {
		t.Fatalf("want exactly 1 ReviewPlan call, got %d", f.planReviewCalls)
	}
	if f.lastActor != "alice" {
		t.Errorf("actor: got %q, want alice", f.lastActor)
	}
	if !strings.Contains(rr.Body.String(), `"verdict":"approved"`) {
		t.Errorf("verdict not surfaced in response: %s", rr.Body.String())
	}

	// GET must NOT be allowed (mutation).
	rr = httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodGet,
		"/api/autonomous/prds/p1/plan-review", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET plan-review: want 405, got %d", rr.Code)
	}
}

// TestAutonomousPRDPlanReviewEndpointNoSurface — a manager wired in
// without the plan-review surface (old binary shape / minimal test
// double) must answer with a clear 400, not a nil-pointer panic.
func TestAutonomousPRDPlanReviewEndpointNoSurface(t *testing.T) {
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: &autonomousConfigPropagationSpy{},
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost,
		"/api/autonomous/prds/p1/plan-review", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "plan review") {
		t.Errorf("error message must explain the missing surface: %s", rr.Body.String())
	}
}
