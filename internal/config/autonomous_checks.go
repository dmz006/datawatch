package config

import (
	"fmt"
	"sort"
	"strings"
)

// Check modes shared by every autonomous.checks key.
const (
	ChecksModeOff    = "off"
	ChecksModeShadow = "shadow"
	ChecksModeOn     = "on"
)

// AutonomousChecksConfig — autonomous.checks sub-config. Each check key is a
// mode: "off" (check not run), "shadow" (run and record findings through the
// triage record, no alert and no block) or "on" (run, record, alert and, for
// the pre-start verifier, block a task start). An empty value means the
// shipped default (on). BlockWhenMissing is nil-able so an absent key means
// true while an explicit false survives. Projects holds per-project overrides
// keyed by project directory; an empty map means every project follows the
// global values.
type AutonomousChecksConfig struct {
	PreStartVerifier string `yaml:"pre_start_verifier" json:"pre_start_verifier"`
	BlockWhenMissing *bool  `yaml:"block_when_missing,omitempty" json:"block_when_missing,omitempty"`
	PlanRules        string `yaml:"plan_rules" json:"plan_rules"`
	AfterDone        string `yaml:"after_done" json:"after_done"`
	RegistryReadme   string `yaml:"registry_readme" json:"registry_readme"`

	Projects map[string]AutonomousChecksOverride `yaml:"projects,omitempty" json:"projects,omitempty"`
}

// AutonomousChecksOverride is one project's override. An empty mode inherits
// the global value; a nil BlockWhenMissing inherits too.
type AutonomousChecksOverride struct {
	PreStartVerifier string `yaml:"pre_start_verifier,omitempty" json:"pre_start_verifier,omitempty"`
	BlockWhenMissing *bool  `yaml:"block_when_missing,omitempty" json:"block_when_missing,omitempty"`
	PlanRules        string `yaml:"plan_rules,omitempty" json:"plan_rules,omitempty"`
	AfterDone        string `yaml:"after_done,omitempty" json:"after_done,omitempty"`
	RegistryReadme   string `yaml:"registry_readme,omitempty" json:"registry_readme,omitempty"`
}

// NormalizeChecksMode trims and lower-cases a mode; empty stays empty.
func NormalizeChecksMode(m string) string { return strings.ToLower(strings.TrimSpace(m)) }

// ValidateChecksMode accepts "", "off", "shadow" and "on" (case-insensitive)
// for the named key and rejects everything else with a clear error.
func ValidateChecksMode(key, mode string) error {
	switch NormalizeChecksMode(mode) {
	case "", ChecksModeOff, ChecksModeShadow, ChecksModeOn:
		return nil
	}
	return fmt.Errorf("autonomous.checks.%s: %q is not a valid mode (must be \"off\", \"shadow\" or \"on\")", key, mode)
}

// Validate checks every mode value, global and per-project.
func (c AutonomousChecksConfig) Validate() error {
	for _, kv := range []struct{ k, v string }{
		{"pre_start_verifier", c.PreStartVerifier},
		{"plan_rules", c.PlanRules},
		{"after_done", c.AfterDone},
		{"registry_readme", c.RegistryReadme},
	} {
		if err := ValidateChecksMode(kv.k, kv.v); err != nil {
			return err
		}
	}
	dirs := make([]string, 0, len(c.Projects))
	for d := range c.Projects {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		o := c.Projects[d]
		for _, kv := range []struct{ k, v string }{
			{"pre_start_verifier", o.PreStartVerifier},
			{"plan_rules", o.PlanRules},
			{"after_done", o.AfterDone},
			{"registry_readme", o.RegistryReadme},
		} {
			if err := ValidateChecksMode(fmt.Sprintf("projects[%q].%s", d, kv.k), kv.v); err != nil {
				return err
			}
		}
	}
	return nil
}
