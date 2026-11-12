package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// snapshotNeverStageYAML writes body to a config file in a temp dir and
// loads it, the same loadProgressYAML pattern the neighbouring config-key
// tests use.
func snapshotNeverStageYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	return loadProgressYAML(t, body)
}

func TestSnapshotNeverStageDefaults(t *testing.T) {
	d := DefaultConfig().Autonomous.SnapshotNeverStage
	if d == nil {
		t.Fatal("DefaultConfig().Autonomous.SnapshotNeverStage is nil, want the shipped default list")
	}
	if !reflect.DeepEqual(d, DefaultSnapshotNeverStage) {
		t.Fatalf("default list = %v, want %v", d, DefaultSnapshotNeverStage)
	}
	// The shipped list must cover scratch, temp and local data folders plus
	// the scratch-file glob.
	for _, want := range []string{"scratch/", "tmp/", "temp/", ".tmp/", "data/", ".data/", "*.tmp"} {
		found := false
		for _, got := range d {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("default list %v is missing %q", d, want)
		}
	}
	// DefaultConfig must hand out a fresh copy, not the package variable:
	// mutating one config must not rewrite the defaults.
	d[0] = "mutated/"
	cfg2 := DefaultConfig().Autonomous.SnapshotNeverStage
	if len(cfg2) != len(DefaultSnapshotNeverStage) || cfg2[0] == "mutated/" {
		t.Fatalf("second DefaultConfig() list = %v; DefaultConfig leaked the mutated slice", cfg2)
	}
}

func TestSnapshotNeverStageLoadFillsDefaults(t *testing.T) {
	// A config file that never mentions the key keeps the shipped defaults.
	cfg, err := snapshotNeverStageYAML(t, "autonomous:\n  enabled: false\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Autonomous.SnapshotNeverStage, DefaultSnapshotNeverStage) {
		t.Fatalf("loaded list = %v, want defaults %v", cfg.Autonomous.SnapshotNeverStage, DefaultSnapshotNeverStage)
	}
	if got := cfg.Autonomous.EffectiveSnapshotNeverStage(); !reflect.DeepEqual(got, DefaultSnapshotNeverStage) {
		t.Fatalf("EffectiveSnapshotNeverStage = %v, want %v", got, DefaultSnapshotNeverStage)
	}
}

func TestSnapshotNeverStageLoadExplicitList(t *testing.T) {
	body := "autonomous:\n  snapshot_never_stage:\n    - scratch/\n    - builds/\n    - \"*.out\"\n"
	cfg, err := snapshotNeverStageYAML(t, body)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scratch/", "builds/", "*.out"}
	if !reflect.DeepEqual(cfg.Autonomous.SnapshotNeverStage, want) {
		t.Fatalf("loaded list = %v, want %v", cfg.Autonomous.SnapshotNeverStage, want)
	}
	if got := cfg.Autonomous.EffectiveSnapshotNeverStage(); !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveSnapshotNeverStage = %v, want %v", got, want)
	}
}

func TestSnapshotNeverStageExplicitEmptyDisables(t *testing.T) {
	cfg, err := snapshotNeverStageYAML(t, "autonomous:\n  snapshot_never_stage: []\n")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Autonomous.SnapshotNeverStage
	if got == nil {
		t.Fatal("explicit empty list loaded as nil; effective state must still read empty, not the defaults")
	}
	if len(got) != 0 {
		t.Fatalf("explicit empty list loaded as %v, want empty non-nil", got)
	}
	if got := cfg.Autonomous.EffectiveSnapshotNeverStage(); len(got) != 0 {
		t.Fatalf("EffectiveSnapshotNeverStage = %v, want empty (defaults disabled)", got)
	}
	// A zero value (never set anywhere) resolves to the shipped defaults.
	var unset AutonomousConfig
	if unset.SnapshotNeverStage != nil {
		t.Fatalf("zero value SnapshotNeverStage = %v, want nil", unset.SnapshotNeverStage)
	}
	if got := unset.EffectiveSnapshotNeverStage(); !reflect.DeepEqual(got, DefaultSnapshotNeverStage) {
		t.Fatalf("zero-value EffectiveSnapshotNeverStage = %v, want %v", got, DefaultSnapshotNeverStage)
	}
}

func TestSnapshotNeverStageRoundTrip(t *testing.T) {
	// The field has no JSON omitempty on purpose: handleAutonomousConfig
	// round-trips the live config through json.Marshal + json.Unmarshal onto
	// an already-populated struct before persisting. The marshal of an
	// explicit "disable the rules" clear must still carry the key, or the
	// clear would silently keep the previously stored list.
	raw, err := json.Marshal(AutonomousConfig{SnapshotNeverStage: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"snapshot_never_stage":[]`) {
		t.Errorf("marshalled clear elided the key (%s): an explicit clear would never reach the saved config", raw)
	}
	// A populated zero struct must round-trip an explicit list unchanged.
	in := AutonomousConfig{SnapshotNeverStage: []string{"scratch/", "*.tmp"}}
	raw, err = json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AutonomousConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.SnapshotNeverStage, in.SnapshotNeverStage) {
		t.Fatalf("round trip = %v, want %v", out.SnapshotNeverStage, in.SnapshotNeverStage)
	}
}

func TestSnapshotNeverStageValidation(t *testing.T) {
	for _, ok := range [][]string{
		nil,
		{},
		{"scratch/", "tmp/", "data/", "*.tmp"},
	} {
		if err := ValidateSnapshotNeverStage(ok); err != nil {
			t.Errorf("ValidateSnapshotNeverStage(%v) = %v, want nil", ok, err)
		}
	}
	for _, bad := range [][]string{
		{""},
		{"  "},
		{"scratch/", ""},
	} {
		if err := ValidateSnapshotNeverStage(bad); err == nil {
			t.Errorf("ValidateSnapshotNeverStage(%v) = nil, want an error naming the blank entry", bad)
		}
	}
	// Load must refuse blank entries in the saved file, not just the
	// accessors.
	if _, err := snapshotNeverStageYAML(t, "autonomous:\n  snapshot_never_stage:\n    - scratch/\n    - \"  \"\n"); err == nil {
		t.Error("Load accepted a blank snapshot_never_stage entry")
	}
}
