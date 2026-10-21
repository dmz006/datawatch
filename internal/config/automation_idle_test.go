package config

import "testing"

func TestAutomationIdleAlertMinutesDefaultsOff(t *testing.T) {
	if got := DefaultConfig().Autonomous.Automation.IdleAlertMinutes; got != 0 {
		t.Fatalf("default idle_alert_minutes=%d, want 0 (off)", got)
	}
}
