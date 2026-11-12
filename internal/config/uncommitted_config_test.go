package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Unset keys must read the operator's defaults (hold, task_files) on a bare
// zero-valued config as well as a loaded one.
func TestUncommittedConfigDefaults(t *testing.T) {
	for name, cfg := range map[string]AutonomousAutomationConfig{
		"zero_value":     {},
		"default_loaded": DefaultConfig().Autonomous.Automation,
		"partial_loaded": loadCleanupYAML(t, "autonomous:\n  enabled: true\n  automation:\n    idle_alert_minutes: 5\n").Autonomous.Automation,
	} {
		t.Run(name, func(t *testing.T) {
			if cfg.UncommittedWorkPolicy != "" {
				t.Errorf("UncommittedWorkPolicy = %q; want unset (empty)", cfg.UncommittedWorkPolicy)
			}
			if cfg.UncommittedWorkScope != "" {
				t.Errorf("UncommittedWorkScope = %q; want unset (empty)", cfg.UncommittedWorkScope)
			}
			if got := cfg.EffectiveUncommittedWorkPolicy(); got != UncommittedWorkPolicyHold {
				t.Errorf("EffectiveUncommittedWorkPolicy = %q; want %q (default)", got, UncommittedWorkPolicyHold)
			}
			if got := cfg.EffectiveUncommittedWorkScope(); got != UncommittedWorkScopeTaskFiles {
				t.Errorf("EffectiveUncommittedWorkScope = %q; want %q (default)", got, UncommittedWorkScopeTaskFiles)
			}
		})
	}
}

// An unknown word is not refused on load: it reads the default.
func TestUncommittedConfigUnknownReadsDefault(t *testing.T) {
	for _, word := range []string{"enabled", "HOLD", "Warn", "true", "worktree-wide", " off "} {
		a := AutonomousAutomationConfig{UncommittedWorkPolicy: word, UncommittedWorkScope: word}
		if got := a.EffectiveUncommittedWorkPolicy(); got != UncommittedWorkPolicyHold {
			t.Errorf("uncommitted_work_policy %q: effective = %q; want %q (unknown reads the default)", word, got, UncommittedWorkPolicyHold)
		}
		if got := a.EffectiveUncommittedWorkScope(); got != UncommittedWorkScopeTaskFiles {
			t.Errorf("uncommitted_work_scope %q: effective = %q; want %q (unknown reads the default)", word, got, UncommittedWorkScopeTaskFiles)
		}
	}

	cfg := loadCleanupYAML(t, "autonomous:\n  automation:\n    idle_alert_minutes: 30\n    uncommitted_work_policy: sometimes\n    uncommitted_work_scope: everything\n")
	a := cfg.Autonomous.Automation
	if got := a.EffectiveUncommittedWorkPolicy(); got != UncommittedWorkPolicyHold {
		t.Errorf("loaded unknown policy: effective = %q; want %q", got, UncommittedWorkPolicyHold)
	}
	if got := a.EffectiveUncommittedWorkScope(); got != UncommittedWorkScopeTaskFiles {
		t.Errorf("loaded unknown scope: effective = %q; want %q", got, UncommittedWorkScopeTaskFiles)
	}
}

// Explicit operator choices are read back, from a bare struct and from a
// loaded YAML file.
func TestUncommittedConfigExplicitValuesReadBack(t *testing.T) {
	for _, want := range []string{UncommittedWorkPolicyHold, UncommittedWorkPolicyWarn, UncommittedWorkPolicyOff} {
		a := AutonomousAutomationConfig{UncommittedWorkPolicy: want}
		if got := a.EffectiveUncommittedWorkPolicy(); got != want {
			t.Errorf("EffectiveUncommittedWorkPolicy with %q = %q; want %q", want, got, want)
		}
	}
	for _, want := range []string{UncommittedWorkScopeTaskFiles, UncommittedWorkScopeWorktree} {
		a := AutonomousAutomationConfig{UncommittedWorkScope: want}
		if got := a.EffectiveUncommittedWorkScope(); got != want {
			t.Errorf("EffectiveUncommittedWorkScope with %q = %q; want %q", want, got, want)
		}
	}

	cfg := loadCleanupYAML(t, `
autonomous:
  automation:
    idle_alert_minutes: 30
    uncommitted_work_policy: warn
    uncommitted_work_scope: worktree
`)
	a := cfg.Autonomous.Automation
	if a.UncommittedWorkPolicy != "warn" || a.UncommittedWorkScope != "worktree" {
		t.Fatalf("raw = policy %q scope %q; want warn and worktree", a.UncommittedWorkPolicy, a.UncommittedWorkScope)
	}
	if got := a.EffectiveUncommittedWorkPolicy(); got != "warn" {
		t.Errorf("EffectiveUncommittedWorkPolicy = %q; want warn", got)
	}
	if got := a.EffectiveUncommittedWorkScope(); got != "worktree" {
		t.Errorf("EffectiveUncommittedWorkScope = %q; want worktree", got)
	}

	cfg = loadCleanupYAML(t, "autonomous:\n  automation:\n    idle_alert_minutes: 30\n    uncommitted_work_policy: off\n")
	if got := cfg.Autonomous.Automation.EffectiveUncommittedWorkPolicy(); got != "off" {
		t.Errorf("EffectiveUncommittedWorkPolicy = %q; want off", got)
	}
}

// Both keys survive a YAML marshal and unmarshal, and unset keys stay out of
// the YAML.
func TestUncommittedConfigYAMLRoundTrip(t *testing.T) {
	in := AutonomousAutomationConfig{
		UncommittedWorkPolicy: UncommittedWorkPolicyWarn,
		UncommittedWorkScope:  UncommittedWorkScopeWorktree,
	}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "uncommitted_work_policy: warn") {
		t.Errorf("marshalled YAML lacks uncommitted_work_policy: warn:\n%s", text)
	}
	if !strings.Contains(text, "uncommitted_work_scope: worktree") {
		t.Errorf("marshalled YAML lacks uncommitted_work_scope: worktree:\n%s", text)
	}
	var out AutonomousAutomationConfig
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.UncommittedWorkPolicy != "warn" || out.UncommittedWorkScope != "worktree" {
		t.Errorf("after round trip policy %q scope %q; want warn and worktree", out.UncommittedWorkPolicy, out.UncommittedWorkScope)
	}

	unset, err := yaml.Marshal(AutonomousAutomationConfig{})
	if err != nil {
		t.Fatalf("Marshal unset: %v", err)
	}
	if strings.Contains(string(unset), "uncommitted_work") {
		t.Errorf("unset keys appear in the YAML:\n%s", unset)
	}
}
