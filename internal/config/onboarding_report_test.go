package config

import "testing"

func TestOnboardingReportConfig_Defaults(t *testing.T) {
	var c OnboardingReportConfig
	if !c.IsZero() || !c.EffectiveEnabled() || !c.EffectiveIncludeNextSteps() || len(c.EffectiveDisabledChecks()) != 0 {
		t.Fatalf("zero block should read enabled, with next steps, nothing disabled: %+v", c)
	}
	f := false
	c = OnboardingReportConfig{Enabled: &f, IncludeNextSteps: &f, DisabledChecks: []string{"memory"}}
	if c.IsZero() || c.EffectiveEnabled() || c.EffectiveIncludeNextSteps() {
		t.Fatal("explicit false must be kept")
	}
	cl := c.Clone()
	cl.DisabledChecks[0] = "x"
	*cl.Enabled = true
	if c.DisabledChecks[0] != "memory" || *c.Enabled {
		t.Fatal("clone aliases the original")
	}
}
