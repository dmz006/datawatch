package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEffectiveGeneratedArtefactsDefaultsOn(t *testing.T) {
	var a AutonomousConfig
	e := a.EffectiveGeneratedArtefacts()
	if !(e.ChangeRecord && e.GateReport && e.ChangelogDraft && e.ParityDraft && e.TrackerRows && e.RegistryRow && e.CombinedDocsTask) {
		t.Fatalf("expected all on, got %+v", e)
	}
}

func TestEffectiveGeneratedArtefactsPerArtefactOff(t *testing.T) {
	off := false
	on := true
	var a AutonomousConfig
	a.GeneratedArtefacts.GateReport = &off
	a.GeneratedArtefacts.RegistryRow = &off
	a.GeneratedArtefacts.ParityDraft = &on
	e := a.EffectiveGeneratedArtefacts()
	if e.GateReport || e.RegistryRow {
		t.Fatalf("gate_report/registry_row should be off: %+v", e)
	}
	if !e.ParityDraft || !e.ChangeRecord || !e.ChangelogDraft || !e.TrackerRows || !e.CombinedDocsTask {
		t.Fatalf("others should stay on: %+v", e)
	}
}

func TestGeneratedArtefactsExplicitOffRoundTrips(t *testing.T) {
	var a AutonomousConfig
	if err := yaml.Unmarshal([]byte("generated_artefacts:\n  tracker_rows: false\n"), &a); err != nil {
		t.Fatal(err)
	}
	if a.EffectiveGeneratedArtefacts().TrackerRows {
		t.Fatal("yaml off lost")
	}
	b, _ := json.Marshal(a)
	var c AutonomousConfig
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	e := c.EffectiveGeneratedArtefacts()
	if e.TrackerRows || !e.ChangeRecord {
		t.Fatalf("json round trip wrong: %+v", e)
	}
}
