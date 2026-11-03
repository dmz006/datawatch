package config

import (
	"fmt"
	"sort"
	"strings"
)

// ShipVersionPlaceholder is the token a ship tag_format must contain; it is
// replaced by the release version when the tag name is built.
const ShipVersionPlaceholder = "{version}"

// Ship defaults.
const (
	ShipDefaultTagFormat = "v" + ShipVersionPlaceholder
	ShipDefaultRemote    = "origin"
)

// AutonomousShipConfig — autonomous.ship sub-config. push, tag and ci_watch
// use the same mode type as autonomous.checks: "off" (skip), "shadow" (plan
// and record the exact command without running it) or "on" (run). An empty
// value means the shipped default (on). Projects holds per-project overrides
// keyed by project directory.
type AutonomousShipConfig struct {
	Push        string   `yaml:"push" json:"push"`
	Tag         string   `yaml:"tag" json:"tag"`
	CIWatch     string   `yaml:"ci_watch" json:"ci_watch"`
	TagFormat   string   `yaml:"tag_format" json:"tag_format"`
	Remote      string   `yaml:"remote" json:"remote"`
	CIWorkflows []string `yaml:"ci_workflows" json:"ci_workflows"`

	Projects map[string]AutonomousShipOverride `yaml:"projects,omitempty" json:"projects,omitempty"`
}

// AutonomousShipOverride is one project's override. An empty/nil field
// inherits the global value; a non-nil CIWorkflows (even empty) replaces it.
type AutonomousShipOverride struct {
	Push        string   `yaml:"push,omitempty" json:"push,omitempty"`
	Tag         string   `yaml:"tag,omitempty" json:"tag,omitempty"`
	CIWatch     string   `yaml:"ci_watch,omitempty" json:"ci_watch,omitempty"`
	TagFormat   string   `yaml:"tag_format,omitempty" json:"tag_format,omitempty"`
	Remote      string   `yaml:"remote,omitempty" json:"remote,omitempty"`
	CIWorkflows []string `yaml:"ci_workflows,omitempty" json:"ci_workflows,omitempty"`
}

// ValidateShipTagFormat accepts "" (default) or a format containing the
// version placeholder.
func ValidateShipTagFormat(key, format string) error {
	if strings.TrimSpace(format) == "" {
		return nil
	}
	if !strings.Contains(format, ShipVersionPlaceholder) {
		return fmt.Errorf("autonomous.ship.%s: %q must contain the version placeholder %s", key, format, ShipVersionPlaceholder)
	}
	return nil
}

func validateShipFields(prefix, push, tag, ciWatch, tagFormat string) error {
	for _, kv := range []struct{ k, v string }{{"push", push}, {"tag", tag}, {"ci_watch", ciWatch}} {
		if err := ValidateChecksMode(prefix+kv.k, kv.v); err != nil {
			return fmt.Errorf("%s", strings.Replace(err.Error(), "autonomous.checks.", "autonomous.ship.", 1))
		}
	}
	return ValidateShipTagFormat(prefix+"tag_format", tagFormat)
}

// Validate checks every mode and tag format, global and per-project.
func (c AutonomousShipConfig) Validate() error {
	if err := validateShipFields("", c.Push, c.Tag, c.CIWatch, c.TagFormat); err != nil {
		return err
	}
	dirs := make([]string, 0, len(c.Projects))
	for d := range c.Projects {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		o := c.Projects[d]
		if err := validateShipFields(fmt.Sprintf("projects[%q].", d), o.Push, o.Tag, o.CIWatch, o.TagFormat); err != nil {
			return err
		}
	}
	return nil
}
