// ci-watch completion guardrail — Configuration Accessibility round-trip:
// PUT /api/config then GET /api/config must show the new value for both
// autonomous.ci_watch_timeout_seconds and the per_prd_guardrails opt-in.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigRoundTrip_CIWatch(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}
	for _, patch := range []map[string]any{
		{"autonomous.ci_watch_timeout_seconds": 1800},
		{"autonomous.per_prd_guardrails": []string{"rules-review", "ci-watch"}},
	} {
		b, _ := json.Marshal(patch)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", patch, rr.Code, rr.Body.String())
		}
	}

	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			CIWatchTimeoutSeconds int      `json:"ci_watch_timeout_seconds"`
			PerPRDGuardrails      []string `json:"per_prd_guardrails"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.CIWatchTimeoutSeconds != 1800 {
		t.Errorf("GET ci_watch_timeout_seconds = %d, want 1800", got.Autonomous.CIWatchTimeoutSeconds)
	}
	if !slices.Equal(got.Autonomous.PerPRDGuardrails, []string{"rules-review", "ci-watch"}) {
		t.Errorf("GET per_prd_guardrails = %v, want [rules-review ci-watch]", got.Autonomous.PerPRDGuardrails)
	}

	// The live manager must receive the change too, not just the YAML.
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if string(fwd["per_prd_guardrails"]) != `["rules-review","ci-watch"]` {
		t.Errorf("per_prd_guardrails not propagated to manager: %s", spy.lastSetConfig)
	}
}

// A negative timeout is rejected and leaves the previous value intact.
func TestApplyConfigPatch_CIWatchTimeoutRejectsNegative(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.ci_watch_timeout_seconds": 900})
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.ci_watch_timeout_seconds": -1})
	if cfg.Autonomous.CIWatchTimeoutSeconds != 900 {
		t.Fatalf("CIWatchTimeoutSeconds = %d, want 900", cfg.Autonomous.CIWatchTimeoutSeconds)
	}
}
