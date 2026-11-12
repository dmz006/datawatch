package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigAutomationAgentInsertPolicyDefault(t *testing.T) {
	if got := DefaultConfig().Autonomous.Automation.AgentInsertPolicy; got != AgentInsertPolicyAllow {
		t.Fatalf("default agent_insert_policy=%q, want %q", got, AgentInsertPolicyAllow)
	}
	var unset AutonomousAutomationConfig
	if got := unset.EffectiveAgentInsertPolicy(); got != "allow" {
		t.Fatalf("unset policy resolves to %q, want allow", got)
	}
}

func TestConfigAutomationAgentInsertPolicyLoadDefaultsToAllow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  automation:\n    idle_alert_minutes: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Autonomous.Automation.AgentInsertPolicy; got != "allow" {
		t.Fatalf("loaded policy=%q, want allow", got)
	}
}

func TestConfigAutomationAgentInsertPolicyRoundTrip(t *testing.T) {
	for _, want := range []string{"allow", "require_operator"} {
		in := AutonomousAutomationConfig{IdleAlertMinutes: 7, AgentInsertPolicy: want}
		raw, err := yaml.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out AutonomousAutomationConfig
		if err := yaml.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		// yaml.v3 round trip turns nil slices into empty non-nil ones;
		// normalize both sides before comparing.
		normalize := func(c AutonomousAutomationConfig) AutonomousAutomationConfig {
			if len(c.RepoCIWatch.Repos) == 0 {
				c.RepoCIWatch.Repos = nil
			}
			if len(c.RepoCIWatch.SecurityWorkflows) == 0 {
				c.RepoCIWatch.SecurityWorkflows = nil
			}
			return c
		}
		if !reflect.DeepEqual(normalize(out), normalize(in)) {
			t.Fatalf("round trip of %q: got %+v, want %+v", want, out, in)
		}
	}
}

func TestConfigAutomationAgentInsertPolicyValidator(t *testing.T) {
	for _, ok := range []string{"", "allow", "require_operator"} {
		if err := ValidateAgentInsertPolicy(ok); err != nil {
			t.Errorf("ValidateAgentInsertPolicy(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"deny", "Allow", "require-operator", " allow", "operator"} {
		if err := ValidateAgentInsertPolicy(bad); err == nil {
			t.Errorf("ValidateAgentInsertPolicy(%q) = nil, want an error", bad)
		}
	}
}

func TestConfigAutomationAgentInsertPolicyLoadRejectsBadValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  automation:\n    agent_insert_policy: deny\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted agent_insert_policy: deny")
	}
}
