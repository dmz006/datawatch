// Package config — daemon-level defaults for registered projects.
//
// The projects_defaults block holds the three settings a project inherits
// when it sets none of its own: branch retention, the weekly share and the
// major-release trigger. The block lives beside the projects map rather than
// inside it, because projects is keyed by project name and a child named
// "defaults" would collide with a project of that name. Every surface
// (config.yaml, GET/PUT /api/config, the comm configure verb, MCP
// config_set/get_config and datawatch config set) uses the dotted key form
// projects_defaults.<field>.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ProjectsDefaultsSettingPrefix is the dotted key prefix of every
// projects_defaults setting.
const ProjectsDefaultsSettingPrefix = "projects_defaults."

// Field names of the projects_defaults block, as they appear in YAML, JSON
// and the dotted keys.
const (
	projectsDefaultsFieldBranchRetentionDays = "branch_retention_days"
	projectsDefaultsFieldWeeklySharePercent  = "weekly_share_percent"
	projectsDefaultsFieldMajorTrigger        = "major_trigger"
)

// ProjectsDefaultsConfig is the projects_defaults block.
//
//   - BranchRetentionDays: days a project's merged branches are kept before
//     cleanup, used when the project sets no branch_retention_days. 0 means
//     unset: branches are kept until the project or operator says otherwise.
//   - WeeklySharePercent: the project's share of the weekly usage budget, used
//     when the project sets no weekly_share. 0 means unset; otherwise 1 to 100.
//   - MajorTrigger: when a major release is raised (manual, milestone or
//     never), used when the project sets no major_trigger. Empty means manual.
type ProjectsDefaultsConfig struct {
	BranchRetentionDays int    `yaml:"branch_retention_days,omitempty" json:"branch_retention_days,omitempty"`
	WeeklySharePercent  int    `yaml:"weekly_share_percent,omitempty" json:"weekly_share_percent,omitempty"`
	MajorTrigger        string `yaml:"major_trigger,omitempty" json:"major_trigger,omitempty"`
}

// EffectiveMajorTrigger returns the configured major trigger, or manual when
// none is set.
func (d ProjectsDefaultsConfig) EffectiveMajorTrigger() string {
	if d.MajorTrigger == "" {
		return MergePipelineDefaultMajorTrigger
	}
	return d.MajorTrigger
}

// Validate reports the first problem with the block, or nil.
func (d ProjectsDefaultsConfig) Validate() error {
	if d.BranchRetentionDays < 0 {
		return fmt.Errorf("%s%s %d is negative", ProjectsDefaultsSettingPrefix, projectsDefaultsFieldBranchRetentionDays, d.BranchRetentionDays)
	}
	if d.WeeklySharePercent < 0 || d.WeeklySharePercent > 100 {
		return fmt.Errorf("%s%s %d is outside 0-100", ProjectsDefaultsSettingPrefix, projectsDefaultsFieldWeeklySharePercent, d.WeeklySharePercent)
	}
	if !projectMajorTriggerValid(d.MajorTrigger) {
		return fmt.Errorf("%s%s %q is not manual, milestone or never", ProjectsDefaultsSettingPrefix, projectsDefaultsFieldMajorTrigger, d.MajorTrigger)
	}
	return nil
}

// ProjectsDefaultsSettingKeys lists every settable dotted key of the block.
func ProjectsDefaultsSettingKeys() []string {
	return []string{
		ProjectsDefaultsSettingPrefix + projectsDefaultsFieldBranchRetentionDays,
		ProjectsDefaultsSettingPrefix + projectsDefaultsFieldWeeklySharePercent,
		ProjectsDefaultsSettingPrefix + projectsDefaultsFieldMajorTrigger,
	}
}

// ValidateProjectsDefaultsSetting checks one projects_defaults.* value before
// it is applied. The value may be a JSON number, a numeric string or, for
// major_trigger, a string. An unknown field name is refused with its name.
func ValidateProjectsDefaultsSetting(key string, value interface{}) error {
	_, err := setProjectsDefaultsField(ProjectsDefaultsConfig{}, key, value)
	return err
}

// ApplyProjectsDefaultsSetting sets one projects_defaults.* key on d after
// the same check ValidateProjectsDefaultsSetting runs. A refused value leaves
// d unchanged and returns the error.
func (d *ProjectsDefaultsConfig) ApplyProjectsDefaultsSetting(key string, value interface{}) error {
	next, err := setProjectsDefaultsField(*d, key, value)
	if err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*d = next
	return nil
}

// setProjectsDefaultsField returns a copy of d with the named field set. It
// checks the value type and range for that field only.
func setProjectsDefaultsField(d ProjectsDefaultsConfig, key string, value interface{}) (ProjectsDefaultsConfig, error) {
	name, ok := strings.CutPrefix(key, ProjectsDefaultsSettingPrefix)
	if !ok {
		return d, fmt.Errorf("%s is not a projects_defaults key", key)
	}
	switch name {
	case projectsDefaultsFieldBranchRetentionDays, projectsDefaultsFieldWeeklySharePercent:
		n, err := projectsDefaultsWholeNumber(key, value)
		if err != nil {
			return d, err
		}
		if name == projectsDefaultsFieldBranchRetentionDays {
			if n < 0 {
				return d, fmt.Errorf("%s must be a whole number of days, 0 or more", key)
			}
			d.BranchRetentionDays = n
		} else {
			if n < 0 || n > 100 {
				return d, fmt.Errorf("%s must be a whole number from 0 to 100", key)
			}
			d.WeeklySharePercent = n
		}
	case projectsDefaultsFieldMajorTrigger:
		s, ok := value.(string)
		if !ok {
			return d, fmt.Errorf("%s must be manual, milestone or never", key)
		}
		s = strings.TrimSpace(s)
		if !projectMajorTriggerValid(s) {
			return d, fmt.Errorf("%s must be manual, milestone or never, got %q", key, s)
		}
		d.MajorTrigger = s
	default:
		return d, fmt.Errorf("unknown projects_defaults key %q (want %s)", key, strings.Join(ProjectsDefaultsSettingKeys(), ", "))
	}
	return d, nil
}

// projectsDefaultsWholeNumber reads a whole number from a JSON number or a
// numeric string.
func projectsDefaultsWholeNumber(key string, value interface{}) (int, error) {
	switch v := value.(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, fmt.Errorf("%s must be a whole number", key)
		}
		return int(v), nil
	case int:
		return v, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, fmt.Errorf("%s must be a whole number", key)
		}
		return n, nil
	}
	return 0, fmt.Errorf("%s must be a whole number", key)
}
