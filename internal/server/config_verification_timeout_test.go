// autonomous.verification_timeout_seconds — config round-trip: the patch
// case stores the value and GET /api/config exposes it. Zero is stored as
// zero here; the 1800 fallback is applied at consumption time.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigVerificationTimeout_RoundTrip(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.verification_timeout_seconds": 900})
	if cfg.Autonomous.VerificationTimeoutSeconds != 900 {
		t.Fatalf("VerificationTimeoutSeconds = %d, want 900", cfg.Autonomous.VerificationTimeoutSeconds)
	}

	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			VerificationTimeoutSeconds int `json:"verification_timeout_seconds"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.VerificationTimeoutSeconds != 900 {
		t.Errorf("GET verification_timeout_seconds = %d, want 900", got.Autonomous.VerificationTimeoutSeconds)
	}
}

// A zero-valued config reads back 0: the default is applied at use, not storage.
func TestConfigVerificationTimeout_ZeroValueReadsBackZero(t *testing.T) {
	cfg := &config.Config{}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			VerificationTimeoutSeconds int `json:"verification_timeout_seconds"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.VerificationTimeoutSeconds != 0 {
		t.Errorf("GET verification_timeout_seconds = %d, want 0", got.Autonomous.VerificationTimeoutSeconds)
	}
	if cfg.Autonomous.VerificationTimeoutSeconds != 0 {
		t.Errorf("stored VerificationTimeoutSeconds = %d, want 0", cfg.Autonomous.VerificationTimeoutSeconds)
	}
}
