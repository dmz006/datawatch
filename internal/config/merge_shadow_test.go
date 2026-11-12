package config

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The defaults are pinned here so a changed default is a deliberate edit to
// the constant block in merge_shadow.go and to this test.
func TestMergeShadowDefaults(t *testing.T) {
	if DefaultMergeShadowEnabled != true || DefaultMergeShadowGateTimeout != 20*time.Minute ||
		DefaultMergeShadowPlanKeep != 200 {
		t.Fatalf("a merge_shadow default changed; update docs/config-reference.yaml and implementation.md with it")
	}

	var zero MergeShadowConfig
	if !zero.EffectiveEnabled() {
		t.Errorf("absent enabled: got false, want true")
	}
	if got := zero.EffectiveGateTimeout(); got != 20*time.Minute {
		t.Errorf("absent gate_timeout: got %v, want 20m", got)
	}
	if got := zero.EffectivePlanKeep(); got != 200 {
		t.Errorf("absent plan_keep: got %d, want 200", got)
	}

	// The zero value of the whole autonomous block also gets the defaults.
	var auto AutonomousConfig
	if !auto.MergeShadow.EffectiveEnabled() || auto.MergeShadow.EffectiveGateTimeout() != 20*time.Minute ||
		auto.MergeShadow.EffectivePlanKeep() != 200 {
		t.Errorf("autonomous zero value does not carry merge_shadow defaults")
	}
}

func TestMergeShadowExplicitValuesOverrideDefaults(t *testing.T) {
	off := false
	gate := "90s"
	keep := 5
	m := MergeShadowConfig{Enabled: &off, GateTimeout: &gate, PlanKeep: &keep}
	if m.EffectiveEnabled() {
		t.Errorf("explicit enabled=false read as on")
	}
	if got := m.EffectiveGateTimeout(); got != 90*time.Second {
		t.Errorf("gate_timeout 90s: got %v", got)
	}
	if got := m.EffectivePlanKeep(); got != 5 {
		t.Errorf("plan_keep 5: got %d", got)
	}
}

func TestMergeShadowInvalidValuesFallBackToDefaults(t *testing.T) {
	for _, g := range []string{"", "soon", "0s", "-5m"} {
		gate := g
		m := MergeShadowConfig{GateTimeout: &gate}
		if got := m.EffectiveGateTimeout(); got != 20*time.Minute {
			t.Errorf("gate_timeout %q: got %v, want default 20m", g, got)
		}
	}
	for _, k := range []int{0, -1} {
		keep := k
		m := MergeShadowConfig{PlanKeep: &keep}
		if got := m.EffectivePlanKeep(); got != 200 {
			t.Errorf("plan_keep %d: got %d, want default 200", k, got)
		}
	}
}

// Explicit false and zero values must survive a YAML round trip, which is why
// the fields are pointers without omitempty.
func TestMergeShadowYAMLRoundTrip(t *testing.T) {
	off := false
	gate := "45m"
	keep := 0
	in := AutonomousConfig{MergeShadow: MergeShadowConfig{Enabled: &off, GateTimeout: &gate, PlanKeep: &keep}}

	data, err := yaml.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AutonomousConfig
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	ms := out.MergeShadow
	if ms.Enabled == nil || *ms.Enabled != false {
		t.Errorf("enabled lost in round trip: %+v\n%s", ms, data)
	}
	if ms.GateTimeout == nil || *ms.GateTimeout != "45m" {
		t.Errorf("gate_timeout lost in round trip: %+v\n%s", ms, data)
	}
	if ms.PlanKeep == nil || *ms.PlanKeep != 0 {
		t.Errorf("plan_keep 0 lost in round trip: %+v\n%s", ms, data)
	}
}

func TestMergeShadowYAMLParsesDocumentedKeys(t *testing.T) {
	src := []byte("autonomous:\n  merge_shadow:\n    enabled: false\n    gate_timeout: 90s\n    plan_keep: 12\n")
	var cfg Config
	if err := yaml.Unmarshal(src, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ms := cfg.Autonomous.MergeShadow
	if ms.EffectiveEnabled() || ms.EffectiveGateTimeout() != 90*time.Second || ms.EffectivePlanKeep() != 12 {
		t.Errorf("documented keys not read: %+v", ms)
	}
}

func TestMergeShadowJSONRoundTrip(t *testing.T) {
	data, err := json.Marshal(MergeShadowConfig{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back MergeShadowConfig
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.EffectiveEnabled() || back.EffectiveGateTimeout() != 20*time.Minute || back.EffectivePlanKeep() != 200 {
		t.Errorf("json round trip of empty config changed defaults: %+v", back)
	}
}
