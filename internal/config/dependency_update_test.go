package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDependencyUpdateDefaults(t *testing.T) {
	var c DependencyUpdateConfig
	if !c.EffectiveEnabled() || c.EffectiveSchedule() != "monday 05:00" || c.EffectiveMaxMajors() != 5 {
		t.Fatalf("bad defaults: %v %q %d", c.EffectiveEnabled(), c.EffectiveSchedule(), c.EffectiveMaxMajors())
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	f, z := false, 0
	c = DependencyUpdateConfig{Enabled: &f, MaxMajors: &z, Schedule: "  friday 23:59 "}
	if c.EffectiveEnabled() || c.EffectiveMaxMajors() != 0 || c.EffectiveSchedule() != "friday 23:59" {
		t.Fatal("explicit values must win")
	}
}

func TestDependencyUpdateSchedule(t *testing.T) {
	for _, ok := range []string{"monday 05:00", "SUNDAY 23:59", "Friday 00:00"} {
		if err := (DependencyUpdateConfig{Schedule: ok}).Validate(); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"mon 05:00", "monday", "monday 5:00", "monday 24:00", "monday 05:60", "monday 05:00 x", "someday 05:00", "monday ab:cd"} {
		err := (DependencyUpdateConfig{Schedule: bad}).Validate()
		if err == nil || !strings.Contains(err.Error(), "weekday HH:MM") {
			t.Errorf("%q: want error naming the form, got %v", bad, err)
		}
	}
}

func TestDependencyUpdateMaxMajorsBounds(t *testing.T) {
	for _, n := range []int{0, 5, 20} {
		n := n
		if err := (DependencyUpdateConfig{MaxMajors: &n}).Validate(); err != nil {
			t.Errorf("%d: %v", n, err)
		}
	}
	for _, n := range []int{-1, 21} {
		n := n
		err := (DependencyUpdateConfig{MaxMajors: &n}).Validate()
		if err == nil || !strings.Contains(err.Error(), "0 to 20") {
			t.Errorf("%d: got %v", n, err)
		}
	}
}

func TestDependencyUpdateYAMLRoundTrip(t *testing.T) {
	f, n := false, 0
	in := DependencyUpdateConfig{Enabled: &f, Schedule: "tuesday 06:30", MaxMajors: &n}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out DependencyUpdateConfig
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.EffectiveEnabled() || out.Schedule != "tuesday 06:30" || out.MaxMajors == nil || *out.MaxMajors != 0 {
		t.Fatalf("round trip lost values: %s", b)
	}
	var empty DependencyUpdateConfig
	if err := yaml.Unmarshal([]byte("{}"), &empty); err != nil || !empty.EffectiveEnabled() {
		t.Fatal("unset must read true")
	}
}

func TestAutonomousAutomationDependencyUpdateWired(t *testing.T) {
	var a AutonomousAutomationConfig
	if err := yaml.Unmarshal([]byte("dependency_update:\n  max_majors: 3\n"), &a); err != nil {
		t.Fatal(err)
	}
	if a.DependencyUpdate.EffectiveMaxMajors() != 3 {
		t.Fatal("block not wired")
	}
}
