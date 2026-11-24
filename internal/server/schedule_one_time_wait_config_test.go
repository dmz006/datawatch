package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// schedule.one_time_max_wait_hours round-trips the REST PUT path: a valid
// value applies, a value below 1 or a non-number is ignored, and the effective
// getter reports 12 when nothing is set.
func TestApplyConfigPatch_ScheduleOneTimeMaxWait(t *testing.T) {
	cfg := &config.Config{}
	if got := cfg.Schedule.EffectiveOneTimeMaxWaitHours(); got != 12 {
		t.Fatalf("default must be 12 hours, got %d", got)
	}
	applyConfigPatch(cfg, map[string]interface{}{"schedule.one_time_max_wait_hours": float64(6)})
	if got := cfg.Schedule.EffectiveOneTimeMaxWaitHours(); got != 6 {
		t.Fatalf("valid value must apply, got %d", got)
	}
	for _, bad := range []interface{}{0, -3, "lots"} {
		applyConfigPatch(cfg, map[string]interface{}{"schedule.one_time_max_wait_hours": bad})
		if got := cfg.Schedule.EffectiveOneTimeMaxWaitHours(); got != 6 {
			t.Fatalf("bad value %v must be ignored, got %d", bad, got)
		}
	}
}
