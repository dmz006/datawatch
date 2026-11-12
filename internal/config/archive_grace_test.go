package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArchiveGraceMinutesDefaultAndExplicitZero(t *testing.T) {
	var unset AutonomousConfig
	if got := unset.ArchiveGraceMinutesOrDefault(); got != 60 {
		t.Fatalf("unset = %d, want 60", got)
	}
	// An explicit 0 must survive a YAML and a JSON round trip.
	zero := 0
	set := AutonomousConfig{ArchiveGraceMinutes: &zero}
	y, err := yaml.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML AutonomousConfig
	if err := yaml.Unmarshal(y, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if got := fromYAML.ArchiveGraceMinutesOrDefault(); got != 0 {
		t.Errorf("yaml round trip of explicit 0 = %d, want 0", got)
	}
	j, _ := json.Marshal(set)
	var fromJSON AutonomousConfig
	if err := json.Unmarshal(j, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if got := fromJSON.ArchiveGraceMinutesOrDefault(); got != 0 {
		t.Errorf("json round trip of explicit 0 = %d, want 0", got)
	}
	ninety := 90
	if got := (AutonomousConfig{ArchiveGraceMinutes: &ninety}).ArchiveGraceMinutesOrDefault(); got != 90 {
		t.Errorf("90 = %d", got)
	}
}

func TestParseArchiveGraceMinutes(t *testing.T) {
	for _, bad := range []any{-1, float64(-1), 1.5, "abc", "1.5", "-3", true, nil} {
		if _, err := ParseArchiveGraceMinutes(bad); err == nil {
			t.Errorf("%v accepted, want refusal", bad)
		}
	}
	for v, want := range map[any]int{0: 0, float64(90): 90, "0": 0, "90": 90} {
		got, err := ParseArchiveGraceMinutes(v)
		if err != nil || got != want {
			t.Errorf("%v = %d, %v; want %d", v, got, err, want)
		}
	}
}
