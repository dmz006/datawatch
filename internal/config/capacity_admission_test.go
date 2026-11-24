package config

import "testing"

func TestCapacityAdmissionDefaults(t *testing.T) {
	var a AutonomousConfig
	if a.CapacityAccountClaudeLimit() != 2 || a.CapacitySupervisorReserveClaude() != 1 ||
		a.CapacitySupervisorReservePi() != 1 || a.CapacitySupervisorReserveHost() != 4 {
		t.Fatalf("defaults wrong: %d %d %d %d", a.CapacityAccountClaudeLimit(), a.CapacitySupervisorReserveClaude(), a.CapacitySupervisorReservePi(), a.CapacitySupervisorReserveHost())
	}
	var nilCfg *AutonomousConfig
	if nilCfg.CapacityAccountClaudeLimit() != 2 || nilCfg.CapacitySupervisorReserveHost() != 4 ||
		nilCfg.CapacitySupervisorReserveClaude() != 1 || nilCfg.CapacitySupervisorReservePi() != 1 {
		t.Fatal("nil receiver must read defaults")
	}
}

func TestCapacityAdmissionExplicitZeroStaysZero(t *testing.T) {
	zero, five := 0, 5
	a := AutonomousConfig{CapacityAccountClaudeLimitSetting: &zero, CapacitySupervisorReserveHostSetting: &five}
	if a.CapacityAccountClaudeLimit() != 0 || a.CapacitySupervisorReserveHost() != 5 {
		t.Fatalf("explicit values lost: %d %d", a.CapacityAccountClaudeLimit(), a.CapacitySupervisorReserveHost())
	}
}

func TestCapacityAdmissionRejectsNegative(t *testing.T) {
	neg := -1
	for _, a := range []AutonomousConfig{
		{CapacityAccountClaudeLimitSetting: &neg},
		{CapacitySupervisorReserveClaudeSetting: &neg},
		{CapacitySupervisorReservePiSetting: &neg},
		{CapacitySupervisorReserveHostSetting: &neg},
	} {
		if validateCapacityAdmission(&a) == nil {
			t.Fatalf("negative value accepted: %+v", a)
		}
	}
	if validateCapacityAdmission(&AutonomousConfig{}) != nil {
		t.Fatal("unset must validate")
	}
}

func TestCapacitySupervisorCredentialDefaultsOn(t *testing.T) {
	var a AutonomousConfig
	if !a.CapacitySupervisorCredentialOn() {
		t.Fatal("unset must read on")
	}
	var nilCfg *AutonomousConfig
	if !nilCfg.CapacitySupervisorCredentialOn() {
		t.Fatal("nil receiver must read on")
	}
	off := false
	a.CapacitySupervisorCredentialEnabled = &off
	if a.CapacitySupervisorCredentialOn() {
		t.Fatal("explicit false must read off")
	}
}
