package config

import (
	"testing"
	"time"
)

func TestGateDrain_Default(t *testing.T) {
	if DefaultGateDrainMinutes != 10 {
		t.Fatalf("default = %d, want 10", DefaultGateDrainMinutes)
	}
	if got := DefaultConfig().Autonomous.GateDrainMinutes; got != 10 {
		t.Fatalf("shipped config = %d, want 10", got)
	}
}

func TestGateDrain_EffectiveAndNormalize(t *testing.T) {
	if NormalizeGateDrainMinutes(-3) != DefaultGateDrainMinutes || NormalizeGateDrainMinutes(0) != 0 || NormalizeGateDrainMinutes(7) != 7 {
		t.Fatal("normalize wrong")
	}
	var nilCfg *AutonomousConfig
	if nilCfg.EffectiveGateDrain() != 10*time.Minute {
		t.Fatal("nil receiver should read the default")
	}
	if (&AutonomousConfig{GateDrainMinutes: 0}).EffectiveGateDrain() != 0 {
		t.Fatal("0 must mean do not wait")
	}
	if (&AutonomousConfig{GateDrainMinutes: 3}).EffectiveGateDrain() != 3*time.Minute {
		t.Fatal("3 minutes expected")
	}
}

func TestGateDrain_ValidateAndParse(t *testing.T) {
	if validateGateDrainMinutes(-1) == nil || validateGateDrainMinutes(0) != nil || validateGateDrainMinutes(10) != nil {
		t.Fatal("validate wrong")
	}
	for _, bad := range []any{-1, -2.0, 1.5, "x", "-4", true, nil} {
		if _, err := ParseGateDrainMinutes(bad); err == nil {
			t.Errorf("%v should be refused", bad)
		}
	}
	for in, want := range map[any]int{0: 0, 5.0: 5, " 12 ": 12} {
		if got, err := ParseGateDrainMinutes(in); err != nil || got != want {
			t.Errorf("%v: got %d, %v", in, got, err)
		}
	}
}
