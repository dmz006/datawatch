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

// verifierRunCapGetResponse mirrors the autonomous block of GET /api/config
// for the one key under test.
type verifierRunCapGetResponse struct {
	Autonomous struct {
		VerifierMaxRunSeconds int `json:"verifier_max_run_seconds"`
	} `json:"autonomous"`
}

// verifierRunCapServer builds a server over cfg with a writable config path,
// so PUT /api/config can persist and GET /api/config can read it back.
func verifierRunCapServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	return &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
}

func verifierRunCapGet(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var got verifierRunCapGetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous.VerifierMaxRunSeconds
}

func verifierRunCapPut(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewBufferString(body)))
	return rr
}

// TestVerifierRunCapRestGetDefault: GET /api/config exposes the 2700-second
// default of autonomous.verifier_max_run_seconds on a default config.
func TestVerifierRunCapRestGetDefault(t *testing.T) {
	s := verifierRunCapServer(t, config.DefaultConfig())
	if got := verifierRunCapGet(t, s); got != 2700 {
		t.Fatalf("GET verifier_max_run_seconds = %d, want 2700", got)
	}
}

// TestVerifierRunCapRestPutZeroAccepted: PUT 0 (no cap) is accepted and reads back as 0.
func TestVerifierRunCapRestPutZeroAccepted(t *testing.T) {
	cfg := config.DefaultConfig()
	s := verifierRunCapServer(t, cfg)
	rr := verifierRunCapPut(t, s, `{"autonomous.verifier_max_run_seconds": 0}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT 0: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if cfg.Autonomous.VerifierMaxRunSeconds != 0 {
		t.Fatalf("stored VerifierMaxRunSeconds = %d, want 0", cfg.Autonomous.VerifierMaxRunSeconds)
	}
	if got := verifierRunCapGet(t, s); got != 0 {
		t.Fatalf("GET after PUT 0 = %d, want 0", got)
	}
}

// TestVerifierRunCapRestPutNegativeRefused: PUT -1 is refused with a 400 that
// names the key, and the stored value is left unchanged.
func TestVerifierRunCapRestPutNegativeRefused(t *testing.T) {
	cfg := config.DefaultConfig()
	s := verifierRunCapServer(t, cfg)
	rr := verifierRunCapPut(t, s, `{"autonomous.verifier_max_run_seconds": -1}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT -1: want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "autonomous.verifier_max_run_seconds") {
		t.Errorf("400 body must name the key, got %q", rr.Body.String())
	}
	if cfg.Autonomous.VerifierMaxRunSeconds != 2700 {
		t.Errorf("refused PUT changed stored value to %d", cfg.Autonomous.VerifierMaxRunSeconds)
	}
}

// TestVerifierRunCapRestPutFractionalRefused: PUT 2.5 is refused with a 400
// that names the key, and the stored value is left unchanged.
func TestVerifierRunCapRestPutFractionalRefused(t *testing.T) {
	cfg := config.DefaultConfig()
	s := verifierRunCapServer(t, cfg)
	rr := verifierRunCapPut(t, s, `{"autonomous.verifier_max_run_seconds": 2.5}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT 2.5: want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "autonomous.verifier_max_run_seconds") {
		t.Errorf("400 body must name the key, got %q", rr.Body.String())
	}
	if cfg.Autonomous.VerifierMaxRunSeconds != 2700 {
		t.Errorf("refused PUT changed stored value to %d", cfg.Autonomous.VerifierMaxRunSeconds)
	}
}
