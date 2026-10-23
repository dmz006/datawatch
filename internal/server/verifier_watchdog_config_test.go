package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestVerifierWatchdogConfigRoundTrip(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.verifier_no_progress_timeout_seconds": 300})
	if cfg.Autonomous.VerifierNoProgressTimeoutSeconds != 300 {
		t.Fatalf("VerifierNoProgressTimeoutSeconds = %d, want 300", cfg.Autonomous.VerifierNoProgressTimeoutSeconds)
	}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			V int `json:"verifier_no_progress_timeout_seconds"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Autonomous.V != 300 {
		t.Errorf("GET verifier_no_progress_timeout_seconds = %d, want 300", got.Autonomous.V)
	}
}
