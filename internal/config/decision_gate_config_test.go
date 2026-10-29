package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDecisionGateConfigAbsentGateReadsTrue(t *testing.T) {
	var q AutonomousQuestionsConfig
	if !q.EffectiveDecisionGate() {
		t.Fatal("absent decision_gate must read true")
	}
	if !DefaultConfig().Autonomous.Questions.EffectiveDecisionGate() {
		t.Fatal("default config decision_gate must read true")
	}
	q.DecisionGate = boolPtr(false)
	if q.EffectiveDecisionGate() {
		t.Fatal("explicit false must read false")
	}
}

func TestDecisionGateConfigAbsentModeReadsShadow(t *testing.T) {
	var q AutonomousQuestionsConfig
	if got := q.EffectiveDecisionGateMode(); got != "shadow" {
		t.Fatalf("absent mode = %q; want shadow", got)
	}
}

func TestDecisionGateConfigModeOnReadsOn(t *testing.T) {
	q := AutonomousQuestionsConfig{DecisionGateMode: "on"}
	if got := q.EffectiveDecisionGateMode(); got != "on" {
		t.Fatalf("mode = %q; want on", got)
	}
}

func TestDecisionGateConfigBadModeRefusedOnLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := "autonomous:\n  questions:\n    decision_gate_mode: enforce\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load accepted decision_gate_mode \"enforce\"")
	}
	if !strings.Contains(err.Error(), "decision_gate_mode") || !strings.Contains(err.Error(), "enforce") {
		t.Errorf("error = %v; want it to name the key and the bad value", err)
	}

	for _, ok := range []string{"shadow", "on"} {
		body := "autonomous:\n  questions:\n    decision_gate_mode: " + ok + "\n"
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load with mode %q: %v", ok, err)
		}
		if got := cfg.Autonomous.Questions.EffectiveDecisionGateMode(); got != ok {
			t.Errorf("mode %q loaded as %q", ok, got)
		}
	}
}

func TestDecisionGateConfigYAMLRoundTrip(t *testing.T) {
	in := AutonomousQuestionsConfig{DecisionGate: boolPtr(false), DecisionGateMode: "on"}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "decision_gate: false") || !strings.Contains(string(data), "decision_gate_mode: \"on\"") &&
		!strings.Contains(string(data), "decision_gate_mode: 'on'") && !strings.Contains(string(data), "decision_gate_mode: on") {
		t.Fatalf("yaml missing keys:\n%s", data)
	}
	var out AutonomousQuestionsConfig
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.DecisionGate == nil || *out.DecisionGate {
		t.Errorf("decision_gate = %v; want explicit false", out.DecisionGate)
	}
	if out.DecisionGateMode != "on" {
		t.Errorf("decision_gate_mode = %q; want on", out.DecisionGateMode)
	}
}
