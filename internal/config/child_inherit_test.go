package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestChildInherit_DefaultTrue(t *testing.T) {
	var c AutonomousAutomationConfig
	if !c.EffectiveChildInherit() {
		t.Fatal("unset child_inherit must read true")
	}
}

func TestChildInherit_ExplicitFalseHonoured(t *testing.T) {
	f := false
	c := AutonomousAutomationConfig{ChildInherit: &f}
	if c.EffectiveChildInherit() {
		t.Fatal("explicit false must be honoured")
	}
	tr := true
	c.ChildInherit = &tr
	if !c.EffectiveChildInherit() {
		t.Fatal("explicit true must be honoured")
	}
}

func TestChildInherit_YAMLRoundTrip(t *testing.T) {
	var c AutonomousAutomationConfig
	if err := yaml.Unmarshal([]byte("child_inherit: false\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.EffectiveChildInherit() {
		t.Fatal("yaml false must read false")
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back AutonomousAutomationConfig
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.ChildInherit == nil || *back.ChildInherit {
		t.Fatalf("round trip lost child_inherit=false: %s", out)
	}
	var unset AutonomousAutomationConfig
	if err := yaml.Unmarshal([]byte("idle_alert_minutes: 5\n"), &unset); err != nil {
		t.Fatal(err)
	}
	if !unset.EffectiveChildInherit() {
		t.Fatal("absent key must read true")
	}
}
