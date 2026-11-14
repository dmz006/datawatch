package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// rtk.write_rules_files must be off unless a person turns it on, so the
// daemon never writes RTK instructions into AGENT.md or CLAUDE.md on its own.
func TestRTKWriteRulesFiles_DefaultsOff(t *testing.T) {
	if DefaultConfig().RTK.WriteRulesFiles {
		t.Fatal("rtk.write_rules_files must default to false")
	}
	var zero Config
	if zero.RTK.WriteRulesFiles {
		t.Fatal("the zero value of rtk.write_rules_files must be false")
	}
}

// The YAML key round-trips through the config file in both directions.
func TestRTKWriteRulesFiles_YAMLRoundTrip(t *testing.T) {
	for _, want := range []bool{true, false} {
		cfg := DefaultConfig()
		cfg.RTK.WriteRulesFiles = want
		raw, err := yaml.Marshal(cfg.RTK)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "write_rules_files:") {
			t.Fatalf("marshalled rtk block lacks write_rules_files:\n%s", raw)
		}
		var back RTKConfig
		if err := yaml.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		if back.WriteRulesFiles != want {
			t.Fatalf("round trip: want %v, got %v", want, back.WriteRulesFiles)
		}
	}
}

// A config file that does not mention the key loads as false.
func TestRTKWriteRulesFiles_AbsentKeyIsFalse(t *testing.T) {
	var parsed struct {
		RTK RTKConfig `yaml:"rtk"`
	}
	if err := yaml.Unmarshal([]byte("rtk:\n  enabled: true\n  auto_init: true\n"), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.RTK.WriteRulesFiles {
		t.Fatal("an absent rtk.write_rules_files must load as false")
	}
}

// The shipped reference config documents the key, defaults it to false, and
// says in its comment that the rules files are written by people only.
func TestRTKWriteRulesFiles_ConfigReference(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "config-reference.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		RTK RTKConfig `yaml:"rtk"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("config-reference.yaml does not parse: %v", err)
	}
	if parsed.RTK.WriteRulesFiles {
		t.Fatal("config-reference.yaml must show rtk.write_rules_files: false")
	}
	if !strings.Contains(string(raw), "write_rules_files: false") {
		t.Fatal("config-reference.yaml lacks write_rules_files: false")
	}
}
