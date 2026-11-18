// Plan records config keys (autonomous.plan_records.*): defaults, an explicit
// false surviving a round trip, the policy validator and the file load
// refusing a bad policy.

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestPlanRecordsWriteThroughDefaultOff asserts an unset config resolves
// write_through to off, and the other keys to their shipped defaults.
func TestPlanRecordsWriteThroughDefaultOff(t *testing.T) {
	for name, p := range map[string]AutonomousPlanRecordsConfig{
		"zero value":     {},
		"default config": DefaultConfig().Autonomous.PlanRecords,
	} {
		if p.EffectiveWriteThrough() {
			t.Errorf("%s: write_through must default to off", name)
		}
		if !p.EffectiveEnabled() {
			t.Errorf("%s: enabled must default to true", name)
		}
		if got := p.EffectiveImportConflictPolicy(); got != "suffix" {
			t.Errorf("%s: import_conflict_policy = %q, want suffix", name, got)
		}
	}
}

func TestPlanRecordsConfigRoundTripExplicitFalse(t *testing.T) {
	f, e, pol := false, false, "skip"
	in := AutonomousPlanRecordsConfig{Enabled: &e, WriteThrough: &f, ImportConflictPolicy: &pol}

	y, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML AutonomousPlanRecordsConfig
	if err := yaml.Unmarshal(y, &fromYAML); err != nil {
		t.Fatal(err)
	}
	j, _ := json.Marshal(in)
	var fromJSON AutonomousPlanRecordsConfig
	if err := json.Unmarshal(j, &fromJSON); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]AutonomousPlanRecordsConfig{"yaml": fromYAML, "json": fromJSON} {
		if got.Enabled == nil || *got.Enabled || got.WriteThrough == nil || *got.WriteThrough {
			t.Errorf("%s: an explicit false was lost: %+v", name, got)
		}
		if got.EffectiveEnabled() || got.EffectiveImportConflictPolicy() != "skip" {
			t.Errorf("%s: effective values wrong: %+v", name, got)
		}
	}
}

func TestPlanRecordsConfigPolicyValidation(t *testing.T) {
	for _, ok := range []string{"", "suffix", "rename", "skip"} {
		if err := ValidatePlanRecordsImportConflictPolicy(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	err := ValidatePlanRecordsImportConflictPolicy("overwrite")
	if err == nil || !strings.Contains(err.Error(), "autonomous.plan_records.import_conflict_policy") {
		t.Fatalf("bad policy must name the key, got %v", err)
	}
}

func TestPlanRecordsConfigLoadRefusesBadPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "autonomous:\n  plan_records:\n    import_conflict_policy: overwrite\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "import_conflict_policy") {
		t.Fatalf("Load must refuse a bad policy naming the key, got %v", err)
	}
}
