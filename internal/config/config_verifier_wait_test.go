package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// countVerifierWait is a tri-state: unset (nil) reads true, and an explicit
// false must survive a marshal/unmarshal round trip — so the field is a
// *bool with omitempty rather than a plain bool that could not tell "never
// set" from "set to false".

func TestVerifierWaitUnsetReadsTrue(t *testing.T) {
	var unset AutonomousLaneFillConfig
	if unset.CountVerifierWait != nil {
		t.Fatalf("an unset count_verifier_wait must be nil, got %v", *unset.CountVerifierWait)
	}
	if !unset.EffectiveCountVerifierWait() {
		t.Fatal("an unset count_verifier_wait must resolve to true")
	}
	defaults := DefaultAutonomousLaneFill()
	if defaults.CountVerifierWait != nil {
		t.Fatalf("the lane fill defaults must leave count_verifier_wait unset, got %v", *defaults.CountVerifierWait)
	}
	if !defaults.EffectiveCountVerifierWait() {
		t.Fatal("the lane fill defaults must resolve count_verifier_wait to true")
	}
}

func TestVerifierWaitExplicitFalseSurvivesYAMLRoundTrip(t *testing.T) {
	f := false
	in := AutonomousLaneFillConfig{Mode: "on", GPULanes: 2, CountVerifierWait: &f}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "count_verifier_wait: false") {
		t.Fatalf("marshalled yaml must carry an explicit false:\n%s", raw)
	}
	var out AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.CountVerifierWait == nil {
		t.Fatal("an explicit count_verifier_wait: false must not collapse to unset")
	}
	if out.EffectiveCountVerifierWait() != false {
		t.Fatal("an explicit false must resolve to false, not the default")
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip changed the block: in=%+v out=%+v", in, out)
	}
}

func TestVerifierWaitExplicitTrueSurvivesYAMLRoundTrip(t *testing.T) {
	tru := true
	in := AutonomousLaneFillConfig{Mode: "off", CountVerifierWait: &tru}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "count_verifier_wait: true") {
		t.Fatalf("marshalled yaml must carry the explicit true:\n%s", raw)
	}
	var out AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.CountVerifierWait == nil || !out.EffectiveCountVerifierWait() {
		t.Fatal("an explicit true must round trip as true")
	}
	if out.Mode != "off" {
		t.Fatalf("round trip changed the sibling mode field: %q", out.Mode)
	}
}

func TestVerifierWaitUnsetDropsOutOfYAML(t *testing.T) {
	raw, err := yaml.Marshal(AutonomousLaneFillConfig{Mode: "shadow", GPULanes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "count_verifier_wait") {
		t.Fatalf("an unset count_verifier_wait must not be written to yaml:\n%s", raw)
	}
	var out AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.CountVerifierWait != nil {
		t.Fatalf("unmarshalling a yaml without the key must leave it unset, got %v", *out.CountVerifierWait)
	}
}

func TestVerifierWaitLoadsFalseFromConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	contents := "autonomous:\n" +
		"  lane_fill:\n" +
		"    mode: on\n" +
		"    gpu_lanes: 2\n" +
		"    claude_lanes: 3\n" +
		"    count_verifier_wait: false\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Autonomous.LaneFill.CountVerifierWait == nil {
		t.Fatal("a config file with count_verifier_wait: false must load it as false, not unset")
	}
	if cfg.Autonomous.LaneFill.EffectiveCountVerifierWait() != false {
		t.Fatal("a config file with count_verifier_wait: false must resolve false")
	}
}
