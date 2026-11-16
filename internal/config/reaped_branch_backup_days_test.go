package config

import "testing"

func TestReapedBranchBackupDaysDefault(t *testing.T) {
	var a AutonomousConfig
	if got := a.EffectiveReapedBranchBackupDays(); got != 30 {
		t.Fatalf("default = %d, want 30", got)
	}
	if ReapedBranchBackupDaysDefault != 30 {
		t.Fatalf("ReapedBranchBackupDaysDefault = %d, want 30", ReapedBranchBackupDaysDefault)
	}
}

func TestReapedBranchBackupDaysNonPositiveFallsBack(t *testing.T) {
	for _, v := range []int{0, -1, -30} {
		a := AutonomousConfig{ReapedBranchBackupDays: v}
		if got := a.EffectiveReapedBranchBackupDays(); got != 30 {
			t.Errorf("value %d: got %d, want 30", v, got)
		}
	}
	a := AutonomousConfig{ReapedBranchBackupDays: 7}
	if got := a.EffectiveReapedBranchBackupDays(); got != 7 {
		t.Errorf("value 7: got %d, want 7", got)
	}
}
