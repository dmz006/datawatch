package config

import "testing"

func TestJudgeDeliveryWaitPerThousandLines_AbsentKeyYieldsDefault(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if got := cfg.Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds; got != 60 {
		t.Fatalf("absent key: judge_delivery_wait_per_thousand_lines_seconds = %d, want 60", got)
	}
	if got := DefaultConfig().Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds; got != 60 {
		t.Fatalf("DefaultConfig value = %d, want 60", got)
	}
}

func TestJudgeDeliveryWaitPerThousandLines_ExplicitZeroRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  judge_delivery_wait_per_thousand_lines_seconds: 0\n")
	if got := cfg.Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds; got != 0 {
		t.Fatalf("explicit 0 loaded as %d, want 0", got)
	}
	back := saveAndReload(t, cfg)
	if got := back.Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds; got != 0 {
		t.Fatalf("explicit 0 after save and load = %d, want 0", got)
	}
}

func TestJudgeDeliveryWaitPerThousandLines_ExplicitValueRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  judge_delivery_wait_per_thousand_lines_seconds: 120\n")
	if got := cfg.Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds; got != 120 {
		t.Fatalf("loaded %d, want 120", got)
	}
	back := saveAndReload(t, cfg)
	if got := back.Autonomous.JudgeDeliveryWaitPerThousandLinesSeconds; got != 120 {
		t.Fatalf("after save and load = %d, want 120", got)
	}
}
