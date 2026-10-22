package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestApplyConfigPatch_TaskDeliveryKeys(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"session.task_delivery_window_seconds": 90,
		"session.task_delivery_attempts":       5,
	})
	if cfg.Session.TaskDeliveryWindowSeconds != 90 {
		t.Fatalf("session.task_delivery_window_seconds not applied: got %d", cfg.Session.TaskDeliveryWindowSeconds)
	}
	if cfg.Session.TaskDeliveryAttempts != 5 {
		t.Fatalf("session.task_delivery_attempts not applied: got %d", cfg.Session.TaskDeliveryAttempts)
	}
	// Effective accessors (what the delivery path reads live).
	if got := cfg.Session.EffectiveTaskDeliveryWindow(); got != 90*time.Second {
		t.Fatalf("EffectiveTaskDeliveryWindow = %v, want 90s", got)
	}
	if got := cfg.Session.EffectiveTaskDeliveryAttempts(); got != 5 {
		t.Fatalf("EffectiveTaskDeliveryAttempts = %d, want 5", got)
	}

	// 0 and negative values must be rejected without touching the fields.
	applyConfigPatch(cfg, map[string]interface{}{
		"session.task_delivery_window_seconds": 0,
		"session.task_delivery_attempts":       -1,
	})
	if cfg.Session.TaskDeliveryWindowSeconds != 90 {
		t.Fatalf("zero window must be rejected: got %d", cfg.Session.TaskDeliveryWindowSeconds)
	}
	if cfg.Session.TaskDeliveryAttempts != 5 {
		t.Fatalf("negative attempts must be rejected: got %d", cfg.Session.TaskDeliveryAttempts)
	}

	// The config GET handler must report the effective values.
	s := &Server{cfg: cfg}
	rec := httptest.NewRecorder()
	s.handleGetConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/config must be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Session map[string]interface{} `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.Session["task_delivery_window_seconds"]; got != float64(90) {
		t.Fatalf("GET session.task_delivery_window_seconds = %v, want 90", got)
	}
	if got := out.Session["task_delivery_attempts"]; got != float64(5) {
		t.Fatalf("GET session.task_delivery_attempts = %v, want 5", got)
	}
}

func TestApplyConfigPatch_TaskDeliveryDefaults(t *testing.T) {
	// Unset fields fall back to defaults via the Effective* accessors.
	cfg := &config.Config{}
	if got := cfg.Session.EffectiveTaskDeliveryWindow(); got <= 0 {
		t.Fatalf("default window must be positive, got %v", got)
	}
	if got := cfg.Session.EffectiveTaskDeliveryAttempts(); got <= 0 {
		t.Fatalf("default attempts must be positive, got %d", got)
	}
}
