package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestVerifierConfigRoundTripAPI(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.verifier_infra_escalation_threshold": 3,
		"autonomous.fallback_verification_backend":       "gemini",
	})
	if cfg.Autonomous.VerifierInfraEscalationThreshold != 3 || cfg.Autonomous.FallbackVerificationBackend != "gemini" {
		t.Fatalf("stored = %d, %q", cfg.Autonomous.VerifierInfraEscalationThreshold, cfg.Autonomous.FallbackVerificationBackend)
	}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			Threshold int    `json:"verifier_infra_escalation_threshold"`
			Fallback  string `json:"fallback_verification_backend"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Autonomous.Threshold != 3 || got.Autonomous.Fallback != "gemini" {
		t.Fatalf("GET = %+v", got.Autonomous)
	}
}

func TestVerifierConfigRoundTripNormalizers(t *testing.T) {
	if config.NormalizeVerifierInfraEscalationThreshold(0) != 2 || config.NormalizeVerifierInfraEscalationThreshold(-1) != 2 || config.NormalizeVerifierInfraEscalationThreshold(4) != 4 {
		t.Fatal("threshold normalizer")
	}
	if config.NormalizeFallbackVerificationBackend("") != "claude-code" || config.NormalizeFallbackVerificationBackend(" pi ") != "pi" {
		t.Fatal("backend normalizer")
	}
}
