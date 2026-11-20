package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArchiveKeepUnmergedDefaultTrue(t *testing.T) {
	if !DefaultArchiveKeepUnmerged {
		t.Fatal("DefaultArchiveKeepUnmerged = false, want true")
	}
	var unset AutonomousConfig
	if !unset.ArchiveKeepUnmergedOrDefault() {
		t.Error("unset = false, want true")
	}
}

func TestArchiveKeepUnmergedExplicitValues(t *testing.T) {
	f, tr := false, true
	if (AutonomousConfig{ArchiveKeepUnmerged: &f}).ArchiveKeepUnmergedOrDefault() {
		t.Error("explicit false read as true")
	}
	if !(AutonomousConfig{ArchiveKeepUnmerged: &tr}).ArchiveKeepUnmergedOrDefault() {
		t.Error("explicit true read as false")
	}
}

func TestArchiveKeepUnmergedFalseSurvivesRoundTrip(t *testing.T) {
	f := false
	set := AutonomousConfig{ArchiveKeepUnmerged: &f}
	y, err := yaml.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML AutonomousConfig
	if err := yaml.Unmarshal(y, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if fromYAML.ArchiveKeepUnmergedOrDefault() {
		t.Error("yaml round trip of explicit false = true")
	}
	j, _ := json.Marshal(set)
	var fromJSON AutonomousConfig
	if err := json.Unmarshal(j, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON.ArchiveKeepUnmergedOrDefault() {
		t.Error("json round trip of explicit false = true")
	}
}
