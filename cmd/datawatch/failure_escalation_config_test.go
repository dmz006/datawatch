package main

import (
	"reflect"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestFailureEscalationConfigMapsToManager(t *testing.T) {
	in := config.FailureEscalationConfig{
		Enabled: false, MaxFailures: 4,
		Steps: []config.FailureEscalationStepConfig{{Backend: "claude-code", Model: "claude-opus-5-5", Effort: "high", Attempts: 2}},
	}
	got := failureEscalationFromConfig(in)
	if got.Enabled == nil || *got.Enabled || got.MaxFailures != 4 {
		t.Fatalf("mapped = %+v", got)
	}
	want := []autonomouspkg.FailureEscalationStep{{Backend: "claude-code", Model: "claude-opus-5-5", Effort: autonomouspkg.EffortHigh, Attempts: 2}}
	if !reflect.DeepEqual(got.Steps, want) {
		t.Fatalf("steps = %+v, want %+v", got.Steps, want)
	}
	in.Steps[0].Backend = "changed"
	if got.Steps[0].Backend != "claude-code" {
		t.Fatal("mapped steps alias the config slice")
	}

	def := failureEscalationFromConfig(config.DefaultFailureEscalationConfig())
	if def.Enabled == nil || !*def.Enabled || def.MaxFailures != 3 || len(def.Steps) != 0 {
		t.Fatalf("default mapped = %+v", def)
	}
}
