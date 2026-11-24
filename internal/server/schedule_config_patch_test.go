package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// The schedule re-arm keys round-trip the REST PUT path: valid values apply,
// an unknown mode or a value below the minimum is ignored, and the effective
// getters report the defaults when nothing is set.
func TestApplyConfigPatch_ScheduleRearm(t *testing.T) {
	cfg := &config.Config{}
	if cfg.Schedule.EffectiveRearmMode() != "next_fire" || cfg.Schedule.EffectiveRearmBackoffSeconds() != 30 ||
		cfg.Schedule.EffectiveRearmBackoffMaxSeconds() != 900 || cfg.Schedule.EffectiveRearmMaxRetries() != 0 {
		t.Fatalf("defaults must keep today's behaviour: %+v", cfg.Schedule)
	}
	applyConfigPatch(cfg, map[string]interface{}{
		"schedule.rearm_mode":                "backoff",
		"schedule.rearm_max_retries":         float64(4),
		"schedule.rearm_backoff_seconds":     float64(10),
		"schedule.rearm_backoff_max_seconds": 120,
	})
	s := cfg.Schedule
	if s.EffectiveRearmMode() != "backoff" || s.EffectiveRearmMaxRetries() != 4 ||
		s.EffectiveRearmBackoffSeconds() != 10 || s.EffectiveRearmBackoffMaxSeconds() != 120 {
		t.Fatalf("valid values must apply: %+v", s)
	}
	applyConfigPatch(cfg, map[string]interface{}{
		"schedule.rearm_mode":                "sometimes",
		"schedule.rearm_max_retries":         -1,
		"schedule.rearm_backoff_seconds":     0,
		"schedule.rearm_backoff_max_seconds": "lots",
	})
	if cfg.Schedule != s {
		t.Fatalf("bad values must be ignored: got %+v want %+v", cfg.Schedule, s)
	}
	applyConfigPatch(cfg, map[string]interface{}{"schedule.rearm_mode": "stay_failed"})
	if cfg.Schedule.EffectiveRearmMode() != "stay_failed" {
		t.Fatalf("stay_failed must apply: %+v", cfg.Schedule)
	}
	// The ceiling is never below the first pause.
	c2 := config.ScheduleConfig{RearmBackoffSeconds: 600, RearmBackoffMaxSeconds: 60}
	if c2.EffectiveRearmBackoffMaxSeconds() != 600 {
		t.Fatalf("ceiling must not be below the first pause: %d", c2.EffectiveRearmBackoffMaxSeconds())
	}
}
