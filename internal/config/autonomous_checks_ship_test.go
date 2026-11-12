package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAutonomousChecksValidateAcceptsModes(t *testing.T) {
	for _, m := range []string{"", "off", "shadow", "on", " ON ", "Shadow"} {
		c := AutonomousChecksConfig{PreStartVerifier: m, PlanRules: m, AfterDone: m, RegistryReadme: m}
		if err := c.Validate(); err != nil {
			t.Errorf("mode %q: unexpected error %v", m, err)
		}
	}
}

func TestAutonomousChecksValidateRejectsBadModeNamingTheKey(t *testing.T) {
	cases := map[string]AutonomousChecksConfig{
		"pre_start_verifier": {PreStartVerifier: "maybe"},
		"plan_rules":         {PlanRules: "maybe"},
		"after_done":         {AfterDone: "maybe"},
		"registry_readme":    {RegistryReadme: "maybe"},
	}
	for key, c := range cases {
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "autonomous.checks."+key) {
			t.Errorf("%s: want error naming the key, got %v", key, err)
		}
	}
}

func TestAutonomousChecksValidateRejectsBadProjectOverride(t *testing.T) {
	c := AutonomousChecksConfig{Projects: map[string]AutonomousChecksOverride{"/p": {AfterDone: "sometimes"}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "/p") {
		t.Fatalf("want error naming the project, got %v", err)
	}
}

func TestAutonomousChecksBlockWhenMissingKeepsExplicitFalse(t *testing.T) {
	var absent, explicit AutonomousChecksConfig
	if err := yaml.Unmarshal([]byte("pre_start_verifier: on\n"), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.BlockWhenMissing != nil {
		t.Fatalf("absent key must stay nil, got %v", *absent.BlockWhenMissing)
	}
	if err := yaml.Unmarshal([]byte("block_when_missing: false\n"), &explicit); err != nil {
		t.Fatal(err)
	}
	if explicit.BlockWhenMissing == nil || *explicit.BlockWhenMissing {
		t.Fatal("explicit false must survive")
	}
	out, err := yaml.Marshal(explicit)
	if err != nil || !strings.Contains(string(out), "block_when_missing: false") {
		t.Fatalf("explicit false lost on round trip: %q %v", out, err)
	}
}

func TestAutonomousShipValidateAcceptsDefaultsAndGoodFormat(t *testing.T) {
	c := AutonomousShipConfig{Push: "on", Tag: "shadow", CIWatch: "off", TagFormat: "release-{version}", Remote: "origin"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (AutonomousShipConfig{}).Validate(); err != nil {
		t.Fatalf("zero value must validate: %v", err)
	}
}

func TestAutonomousShipValidateRejectsBadModeAndFormat(t *testing.T) {
	for key, c := range map[string]AutonomousShipConfig{
		"push":     {Push: "x"},
		"tag":      {Tag: "x"},
		"ci_watch": {CIWatch: "x"},
	} {
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "autonomous.ship."+key) {
			t.Errorf("%s: want error naming autonomous.ship.%s, got %v", key, key, err)
		}
	}
	err := AutonomousShipConfig{TagFormat: "release"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "{version}") {
		t.Fatalf("tag_format without placeholder must be rejected, got %v", err)
	}
}

func TestAutonomousShipValidateRejectsBadProjectOverride(t *testing.T) {
	c := AutonomousShipConfig{Projects: map[string]AutonomousShipOverride{"/p": {TagFormat: "nope"}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "/p") {
		t.Fatalf("want error naming the project, got %v", err)
	}
}

func TestAutonomousShipCIWorkflowsYAMLRoundTrip(t *testing.T) {
	var c AutonomousShipConfig
	if err := yaml.Unmarshal([]byte("ci_workflows: [release, ci]\nremote: upstream\n"), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.CIWorkflows) != 2 || c.CIWorkflows[0] != "release" || c.Remote != "upstream" {
		t.Fatalf("unexpected parse: %+v", c)
	}
}
