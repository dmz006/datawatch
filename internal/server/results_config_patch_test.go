package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// results.schedule_history_limit must round-trip the REST PUT
// (applyConfigPatch) path: valid values apply, values below 1 are
// ignored so the effective limit never drops below 1, and the GET
// surface always reports the effective value.
func TestApplyConfigPatch_ResultsScheduleHistoryLimit(t *testing.T) {
	cfg := &config.Config{}

	applyConfigPatch(cfg, map[string]interface{}{"results.schedule_history_limit": float64(5)})
	if cfg.Results.ScheduleHistoryLimit != 5 {
		t.Fatalf("results.schedule_history_limit: got %d, want 5", cfg.Results.ScheduleHistoryLimit)
	}
	applyConfigPatch(cfg, map[string]interface{}{"results.schedule_history_limit": 2})
	if cfg.Results.ScheduleHistoryLimit != 2 {
		t.Fatalf("int value: got %d, want 2", cfg.Results.ScheduleHistoryLimit)
	}
	if got := cfg.Results.EffectiveScheduleHistoryLimit(); got != 2 {
		t.Fatalf("effective: got %d, want 2", got)
	}

	// Below-1 values are rejected: the value must not change.
	applyConfigPatch(cfg, map[string]interface{}{"results.schedule_history_limit": 0})
	if cfg.Results.ScheduleHistoryLimit != 2 {
		t.Fatalf("zero value must be ignored: got %d, want 2", cfg.Results.ScheduleHistoryLimit)
	}
	applyConfigPatch(cfg, map[string]interface{}{"results.schedule_history_limit": -3})
	if cfg.Results.ScheduleHistoryLimit != 2 {
		t.Fatalf("negative value must be ignored: got %d, want 2", cfg.Results.ScheduleHistoryLimit)
	}
	// Non-numeric junk must also be ignored.
	applyConfigPatch(cfg, map[string]interface{}{"results.schedule_history_limit": "lots"})
	if cfg.Results.ScheduleHistoryLimit != 2 {
		t.Fatalf("non-numeric value must be ignored: got %d, want 2", cfg.Results.ScheduleHistoryLimit)
	}

	// Default when unset: effective limit is 20.
	fresh := &config.Config{}
	if got := fresh.Results.EffectiveScheduleHistoryLimit(); got != 20 {
		t.Fatalf("default effective limit: got %d, want 20", got)
	}
}
