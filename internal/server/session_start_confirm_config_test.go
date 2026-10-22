// autonomous.session_start_confirm_seconds — config round-trip: the patch
// case stores the value and GET /api/config exposes it. Zero is stored as
// zero here; the 120-second fallback (SessionStartConfirmDefaultSeconds)
// is applied at consumption time via
// config.NormalizeSessionStartConfirmSeconds.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigSessionStartConfirm_RoundTrip(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.session_start_confirm_seconds": 300})
	if cfg.Autonomous.SessionStartConfirmSeconds != 300 {
		t.Fatalf("SessionStartConfirmSeconds = %d, want 300", cfg.Autonomous.SessionStartConfirmSeconds)
	}

	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			SessionStartConfirmSeconds int `json:"session_start_confirm_seconds"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.SessionStartConfirmSeconds != 300 {
		t.Errorf("GET session_start_confirm_seconds = %d, want 300", got.Autonomous.SessionStartConfirmSeconds)
	}
}

// A zero-valued config reads back 0: the default is applied at use, not storage.
func TestConfigSessionStartConfirm_ZeroValueReadsBackZero(t *testing.T) {
	cfg := &config.Config{}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			SessionStartConfirmSeconds int `json:"session_start_confirm_seconds"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.SessionStartConfirmSeconds != 0 {
		t.Errorf("GET session_start_confirm_seconds = %d, want 0", got.Autonomous.SessionStartConfirmSeconds)
	}
	if cfg.Autonomous.SessionStartConfirmSeconds != 0 {
		t.Errorf("stored SessionStartConfirmSeconds = %d, want 0", cfg.Autonomous.SessionStartConfirmSeconds)
	}
}

// Zero/negative values are accepted on write and normalized to the 120
// default at read time, exactly like session_idle_timeout_seconds.
func TestConfigSessionStartConfirm_ZeroAndNegativeNormalizeOnRead(t *testing.T) {
	if got := config.NormalizeSessionStartConfirmSeconds(0); got != config.SessionStartConfirmDefaultSeconds {
		t.Errorf("Normalize(0) = %d, want %d", got, config.SessionStartConfirmDefaultSeconds)
	}
	if got := config.NormalizeSessionStartConfirmSeconds(-10); got != config.SessionStartConfirmDefaultSeconds {
		t.Errorf("Normalize(-10) = %d, want %d", got, config.SessionStartConfirmDefaultSeconds)
	}
	if got := config.NormalizeSessionStartConfirmSeconds(60); got != 60 {
		t.Errorf("Normalize(60) = %d, want 60", got)
	}
}
