package config

import "testing"

func TestFinishedWorkModeDefaultsToOn(t *testing.T) {
	for in, want := range map[string]string{"": "on", "on": "on", "ON": "on", "shadow": "shadow", " off ": "off", "bogus": "on"} {
		if got := (AutonomousConfig{FinishedWork: in}).EffectiveFinishedWork(); got != want {
			t.Errorf("EffectiveFinishedWork(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateFinishedWork(t *testing.T) {
	for _, ok := range []string{"", "on", "shadow", "off", "OFF"} {
		if err := ValidateFinishedWork(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	if err := ValidateFinishedWork("maybe"); err == nil {
		t.Error("maybe accepted")
	}
}
