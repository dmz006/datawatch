package config

import "testing"

func TestPreserveAttemptWorkDefaultsTrue(t *testing.T) {
	var unset AutonomousConfig
	if !unset.PreserveAttemptWorkEnabled() {
		t.Fatal("unset preserve_attempt_work must read as true")
	}
	if DefaultConfig().Autonomous.PreserveAttemptWork != nil {
		t.Fatal("default config must leave preserve_attempt_work unset (nil)")
	}
	if !DefaultConfig().Autonomous.PreserveAttemptWorkEnabled() {
		t.Fatal("default config must keep preserve_attempt_work enabled")
	}
	off := false
	explicit := AutonomousConfig{PreserveAttemptWork: &off}
	if explicit.PreserveAttemptWorkEnabled() {
		t.Fatal("explicit false must read as false")
	}
}
