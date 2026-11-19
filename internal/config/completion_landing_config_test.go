package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCompletionLandingConfigDefault(t *testing.T) {
	var c CompletionLandingConfig
	if !c.EffectiveEnabled() {
		t.Fatal("unset completion landing gate must read on")
	}
	var cfg Config
	if !cfg.Autonomous.CompletionLanding.EffectiveEnabled() {
		t.Fatal("zero Config must read the gate on")
	}
}

func TestCompletionLandingConfigExplicitValues(t *testing.T) {
	f, tr := false, true
	if (CompletionLandingConfig{Enabled: &f}).EffectiveEnabled() {
		t.Fatal("explicit false must read off")
	}
	if !(CompletionLandingConfig{Enabled: &tr}).EffectiveEnabled() {
		t.Fatal("explicit true must read on")
	}
}

func TestCompletionLandingConfigRoundTrip(t *testing.T) {
	var c CompletionLandingConfig
	if err := yaml.Unmarshal([]byte("enabled: false\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.EffectiveEnabled() {
		t.Fatal("yaml false must read off")
	}
	b, _ := json.Marshal(c)
	var back CompletionLandingConfig
	if err := json.Unmarshal(b, &back); err != nil || back.EffectiveEnabled() {
		t.Fatalf("json round trip lost false: %s %v", b, err)
	}
	var empty CompletionLandingConfig
	if err := yaml.Unmarshal([]byte("{}\n"), &empty); err != nil || !empty.EffectiveEnabled() {
		t.Fatal("empty block must read on")
	}
}
