package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// stalled_merge_minutes is a tri-state: unset (nil) reads 30, an explicit 0
// turns the rule off, and a negative value is rejected. The field is a *int
// with omitempty so an explicit 0 survives a round trip.

func TestStalledMergeMinutesUnsetReadsThirty(t *testing.T) {
	var unset AutonomousLaneFillConfig
	if unset.StalledMergeMinutes != nil {
		t.Fatalf("an unset stalled_merge_minutes must be nil, got %d", *unset.StalledMergeMinutes)
	}
	if got := unset.EffectiveStalledMergeMinutes(); got != 30 {
		t.Fatalf("an unset stalled_merge_minutes must read 30, got %d", got)
	}
	defaults := DefaultAutonomousLaneFill()
	if got := defaults.EffectiveStalledMergeMinutes(); got != 30 {
		t.Fatalf("the lane fill defaults must read 30, got %d", got)
	}
	if got := defaults.EffectiveStalledMergeMinutes(); got != DefaultLaneFillStalledMergeMinutes {
		t.Fatalf("the default must match the shipped constant, got %d", got)
	}
}

func TestStalledMergeMinutesExplicitZeroTurnsRuleOff(t *testing.T) {
	zero := 0
	c := AutonomousLaneFillConfig{StalledMergeMinutes: &zero}
	if got := c.EffectiveStalledMergeMinutes(); got != 0 {
		t.Fatalf("an explicit 0 must read 0 (rule off), got %d", got)
	}
	if err := c.validate(); err != nil {
		t.Fatalf("an explicit 0 must pass validation: %v", err)
	}
}

func TestStalledMergeMinutesExplicitValueIsKept(t *testing.T) {
	n := 45
	c := AutonomousLaneFillConfig{StalledMergeMinutes: &n}
	if got := c.EffectiveStalledMergeMinutes(); got != 45 {
		t.Fatalf("an explicit 45 must read 45, got %d", got)
	}
}

func TestStalledMergeMinutesNegativeRejected(t *testing.T) {
	neg := -1
	c := AutonomousLaneFillConfig{StalledMergeMinutes: &neg}
	err := c.validate()
	if err == nil {
		t.Fatal("a negative stalled_merge_minutes must be rejected by validation")
	}
	if !strings.Contains(err.Error(), "stalled_merge_minutes") {
		t.Fatalf("the error must name the key, got: %v", err)
	}
}

func TestStalledMergeMinutesNegativeRejectedOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	contents := "autonomous:\n" +
		"  lane_fill:\n" +
		"    mode: shadow\n" +
		"    stalled_merge_minutes: -5\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a config file with a negative stalled_merge_minutes must fail to load")
	}
}

func TestStalledMergeMinutesZeroSurvivesRoundTrips(t *testing.T) {
	zero := 0
	in := AutonomousLaneFillConfig{Mode: "shadow", StalledMergeMinutes: &zero}

	rawYAML, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawYAML), "stalled_merge_minutes: 0") {
		t.Fatalf("marshalled yaml must carry an explicit 0:\n%s", rawYAML)
	}
	var fromYAML AutonomousLaneFillConfig
	if err := yaml.Unmarshal(rawYAML, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if fromYAML.StalledMergeMinutes == nil || fromYAML.EffectiveStalledMergeMinutes() != 0 {
		t.Fatalf("yaml round trip lost the explicit 0: %+v", fromYAML.StalledMergeMinutes)
	}

	rawJSON, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawJSON), `"stalled_merge_minutes":0`) {
		t.Fatalf("marshalled json must carry an explicit 0:\n%s", rawJSON)
	}
	var fromJSON AutonomousLaneFillConfig
	if err := json.Unmarshal(rawJSON, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON.StalledMergeMinutes == nil || fromJSON.EffectiveStalledMergeMinutes() != 0 {
		t.Fatalf("json round trip lost the explicit 0: %+v", fromJSON.StalledMergeMinutes)
	}
}

func TestStalledMergeMinutesUnsetDropsOutOfYAML(t *testing.T) {
	raw, err := yaml.Marshal(AutonomousLaneFillConfig{Mode: "shadow", GPULanes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "stalled_merge_minutes") {
		t.Fatalf("an unset stalled_merge_minutes must not be written to yaml:\n%s", raw)
	}
	var out AutonomousLaneFillConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.StalledMergeMinutes != nil || out.EffectiveStalledMergeMinutes() != 30 {
		t.Fatalf("a yaml without the key must stay unset and read 30, got %v", out.StalledMergeMinutes)
	}
}

func TestStalledMergeMinutesLoadsZeroFromConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	contents := "autonomous:\n" +
		"  lane_fill:\n" +
		"    mode: shadow\n" +
		"    stalled_merge_minutes: 0\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	lf := cfg.Autonomous.LaneFill
	if lf.StalledMergeMinutes == nil || lf.EffectiveStalledMergeMinutes() != 0 {
		t.Fatalf("a config file with stalled_merge_minutes: 0 must load it as 0, not unset: %v", lf.StalledMergeMinutes)
	}
}
