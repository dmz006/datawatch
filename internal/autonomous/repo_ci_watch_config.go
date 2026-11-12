package autonomous

import (
	"errors"
	"fmt"
	"strings"
)

// Repo CI watch (autonomous.automation.repo_ci_watch) — the knobs that let
// the coordinator watch the CI runs of external repositories. Every
// default is applied in exactly one place: the Effective* accessors in
// this file. No call site may hard-code a default; callers read the
// effective value.

// RepoCIWatchMode — the legal repo CI watch modes. An empty (unset) mode
// reads RepoCIWatchModeOff; any other value is refused on every write
// path (REST PUT, the dedicated autonomous config surface and config
// load all reject it before it is stored).
const (
	RepoCIWatchModeOff    = "off"
	RepoCIWatchModeShadow = "shadow"
	RepoCIWatchModeOn     = "on"
)

const (
	// RepoCIWatchDefaultPollSeconds — how often (seconds) a watched
	// repository is polled for new CI runs. Minimum 60.
	RepoCIWatchDefaultPollSeconds = 300
	// RepoCIWatchMinPollSeconds — minimum poll interval; a value below it
	// is refused on every write path.
	RepoCIWatchMinPollSeconds = 60
	// RepoCIWatchDefaultTagCount — how many of the newest release tags a
	// watched repository is tracked with.
	RepoCIWatchDefaultTagCount = 5
	// RepoCIWatchDefaultHungAfterMinutes — a CI run older than this many
	// whole minutes is reported as hung. Minimum 1.
	RepoCIWatchDefaultHungAfterMinutes = 60
	// RepoCIWatchMinHungAfterMinutes — minimum hung threshold; a value
	// below it is refused on every write path.
	RepoCIWatchMinHungAfterMinutes = 1
	// RepoCIWatchDeleteSupersededReport — report a superseded run without
	// deleting it. The default, and what an empty value reads.
	RepoCIWatchDeleteSupersededReport = "report"
	// RepoCIWatchDeleteSupersededDelete — delete the superseded run after
	// the newer one is recorded.
	RepoCIWatchDeleteSupersededDelete = "delete"
)

// DefaultRepoCIWatchSecurityWorkflows is the default list of workflow
// names to watch for the security signals. An unset (nil) list reads this
// value; an explicitly empty list means no security workflows are watched
// at all.
var DefaultRepoCIWatchSecurityWorkflows = []string{
	"CodeQL",
	"dependency-review",
	"owasp-zap",
	"secret-scan",
	"security-scan",
}

// RepoCIWatchConfig — the repo CI watch knobs
// (autonomous.automation.repo_ci_watch.*).
//
//   - Mode: off (default; empty also reads off), shadow or on. Any other
//     value is refused on every write path.
//   - Repos: absolute repository paths to watch. Empty (or unset) means
//     nothing is watched; a repository is never inferred from the working
//     directory, and a non-absolute path is refused on every write path.
//   - PollSeconds: whole seconds, minimum 60; an unset (0) value reads the
//     RepoCIWatchDefaultPollSeconds (300) default.
//   - AlertMinutes: pointer on purpose. Unset (nil) reads
//     DefaultAutomationTriageAlertMinutes (30); an explicit 0 turns the
//     alert off and survives a merge round trip; a negative value is
//     refused on every write path.
//   - TagCount: pointer on purpose. Unset (nil) reads
//     RepoCIWatchDefaultTagCount (5); an explicit 0 means no tags are
//     watched; a negative value is refused on every write path.
//   - HungAfterMinutes: whole minutes, minimum 1; an unset (0) value
//     reads the RepoCIWatchDefaultHungAfterMinutes (60) default.
//   - SecurityWorkflows: workflow names to watch. Unset (nil) reads
//     DefaultRepoCIWatchSecurityWorkflows; an explicitly empty list means
//     none.
//   - DeleteSuperseded: report (default; empty also reads report) or
//     delete. Any other value is refused on every write path.
type RepoCIWatchConfig struct {
	Mode              string   `yaml:"mode" json:"mode"`
	Repos             []string `yaml:"repos" json:"repos"`
	PollSeconds       int      `yaml:"poll_seconds" json:"poll_seconds"`
	AlertMinutes      *int     `yaml:"alert_minutes,omitempty" json:"alert_minutes,omitempty"`
	TagCount          *int     `yaml:"tag_count,omitempty" json:"tag_count,omitempty"`
	HungAfterMinutes  int      `yaml:"hung_after_minutes" json:"hung_after_minutes"`
	SecurityWorkflows []string `yaml:"security_workflows" json:"security_workflows"`
	DeleteSuperseded  string   `yaml:"delete_superseded" json:"delete_superseded"`
}

// EffectiveMode returns the mode in effect: an empty (unset) value reads
// RepoCIWatchModeOff. Any value other than off, shadow or on is refused
// on the write paths, so this never has to guess for it.
func (c RepoCIWatchConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return RepoCIWatchModeOff
	}
	return mode
}

// EffectiveRepos returns the repository paths to watch: the configured
// list, or an empty (non-nil) slice when unset — nothing is watched. A
// repository is never inferred from the working directory.
func (c RepoCIWatchConfig) EffectiveRepos() []string {
	if c.Repos == nil {
		return []string{}
	}
	return c.Repos
}

// EffectivePollSeconds returns the poll interval (seconds) in effect: an
// unset (0) value reads the RepoCIWatchDefaultPollSeconds (300) default.
// A value below RepoCIWatchMinPollSeconds (60, and 0) is refused on the
// write paths, so this never has to guess for them.
func (c RepoCIWatchConfig) EffectivePollSeconds() int {
	if c.PollSeconds == 0 {
		return RepoCIWatchDefaultPollSeconds
	}
	return c.PollSeconds
}

// EffectiveAlertMinutes returns the alert threshold in effect: an unset
// (nil) value reads DefaultAutomationTriageAlertMinutes (30); an explicit
// 0 turns the alert off. Negative values are refused on the write paths,
// so this never has to guess for them.
func (c RepoCIWatchConfig) EffectiveAlertMinutes() int {
	if c.AlertMinutes == nil {
		return DefaultAutomationTriageAlertMinutes
	}
	return *c.AlertMinutes
}

// EffectiveTagCount returns how many of the newest release tags to watch:
// an unset (nil) value reads the RepoCIWatchDefaultTagCount (5) default;
// an explicit 0 means none. Negative values are refused on the write
// paths, so this never has to guess for them.
func (c RepoCIWatchConfig) EffectiveTagCount() int {
	if c.TagCount == nil {
		return RepoCIWatchDefaultTagCount
	}
	return *c.TagCount
}

// EffectiveHungAfterMinutes returns the hung-run threshold (whole minutes)
// in effect: an unset (0) value reads the
// RepoCIWatchDefaultHungAfterMinutes (60) default. A value below
// RepoCIWatchMinHungAfterMinutes (1, and 0) is refused on the write
// paths, so this never has to guess for them.
func (c RepoCIWatchConfig) EffectiveHungAfterMinutes() int {
	if c.HungAfterMinutes == 0 {
		return RepoCIWatchDefaultHungAfterMinutes
	}
	return c.HungAfterMinutes
}

// EffectiveSecurityWorkflows returns the workflow names to watch: the
// configured list (an explicitly empty list means none), or
// DefaultRepoCIWatchSecurityWorkflows when unset (nil).
func (c RepoCIWatchConfig) EffectiveSecurityWorkflows() []string {
	if c.SecurityWorkflows == nil {
		return DefaultRepoCIWatchSecurityWorkflows
	}
	return c.SecurityWorkflows
}

// EffectiveDeleteSuperseded returns the superseded-run policy in effect:
// an empty (unset) value reads RepoCIWatchDeleteSupersededReport. Any
// value other than report or delete is refused on the write paths, so
// this never has to guess for it.
func (c RepoCIWatchConfig) EffectiveDeleteSuperseded() string {
	v := strings.ToLower(strings.TrimSpace(c.DeleteSuperseded))
	if v == "" {
		return RepoCIWatchDeleteSupersededReport
	}
	return v
}

// DefaultRepoCIWatchConfig returns a RepoCIWatchConfig fully populated
// with the defaults every Effective* accessor applies, so callers can
// start from a known-correct value without remembering any default.
func DefaultRepoCIWatchConfig() RepoCIWatchConfig {
	tags := RepoCIWatchDefaultTagCount
	alert := DefaultAutomationTriageAlertMinutes
	return RepoCIWatchConfig{
		Mode:             RepoCIWatchModeOff,
		PollSeconds:      RepoCIWatchDefaultPollSeconds,
		AlertMinutes:     &alert,
		TagCount:         &tags,
		HungAfterMinutes: RepoCIWatchDefaultHungAfterMinutes,
		SecurityWorkflows: append([]string(nil),
			DefaultRepoCIWatchSecurityWorkflows...),
		DeleteSuperseded: RepoCIWatchDeleteSupersededReport,
	}
}

// ValidateRepoCIWatchMode accepts the three legal modes (off, shadow, on;
// case-insensitive, trimmed; empty = unset = off) and rejects every other
// value with a clear error naming the accepted set.
func ValidateRepoCIWatchMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", RepoCIWatchModeOff, RepoCIWatchModeShadow, RepoCIWatchModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.repo_ci_watch.mode: %q is not a valid value (must be %q, %q or %q)",
			mode, RepoCIWatchModeOff, RepoCIWatchModeShadow, RepoCIWatchModeOn)
	}
}

// ValidateRepoCIWatchDeleteSuperseded accepts the two legal
// delete_superseded values (report, delete; case-insensitive, trimmed;
// empty = unset = report) and rejects every other value with a clear
// error naming the accepted set.
func ValidateRepoCIWatchDeleteSuperseded(v string) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", RepoCIWatchDeleteSupersededReport, RepoCIWatchDeleteSupersededDelete:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.repo_ci_watch.delete_superseded: %q is not a valid value (must be %q or %q)",
			v, RepoCIWatchDeleteSupersededReport, RepoCIWatchDeleteSupersededDelete)
	}
}

// validateRepoCIWatchRepos accepts each repository path only when it is
// a plain non-empty absolute path; a repository is never inferred from the
// working directory, so a non-absolute value is refused.
func validateRepoCIWatchRepos(repos []string) error {
	for _, r := range repos {
		path := strings.TrimSpace(r)
		if len(path) <= 1 || !strings.HasPrefix(path, "/") {
			return fmt.Errorf("autonomous.automation.repo_ci_watch.repos: %q must be an absolute path (repositories are never inferred from the working directory)", r)
		}
	}
	return nil
}

// ValidateRepoCIWatchConfig checks a whole RepoCIWatchConfig the same way
// the write paths do. Every write path runs these same rules, so a bad
// value is refused consistently rather than normalized silently.
func ValidateRepoCIWatchConfig(c RepoCIWatchConfig) error {
	if err := ValidateRepoCIWatchMode(c.Mode); err != nil {
		return err
	}
	if err := validateRepoCIWatchRepos(c.Repos); err != nil {
		return err
	}
	if p := c.EffectivePollSeconds(); c.PollSeconds > 0 && p < RepoCIWatchMinPollSeconds {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.poll_seconds: %d must be a whole number >= %d (unset reads the %d-second default)",
			c.PollSeconds, RepoCIWatchMinPollSeconds, RepoCIWatchDefaultPollSeconds)
	}
	if a := c.EffectiveAlertMinutes(); a < 0 {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.alert_minutes: %d must be a whole number >= 0 (0 = alert off, unset reads the %d-minute default)",
			a, DefaultAutomationTriageAlertMinutes)
	}
	if c.TagCount != nil && *c.TagCount < 0 {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.tag_count: %d must be a whole number >= 0 (0 = none, unset reads the %d default)",
			*c.TagCount, RepoCIWatchDefaultTagCount)
	}
	if h := c.EffectiveHungAfterMinutes(); c.HungAfterMinutes > 0 && h < RepoCIWatchMinHungAfterMinutes {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.hung_after_minutes: %d must be a whole number >= %d (unset reads the %d-minute default)",
			c.HungAfterMinutes, RepoCIWatchMinHungAfterMinutes, RepoCIWatchDefaultHungAfterMinutes)
	}
	if err := ValidateRepoCIWatchDeleteSuperseded(c.DeleteSuperseded); err != nil {
		return err
	}
	for _, w := range c.SecurityWorkflows {
		if strings.TrimSpace(w) == "" {
			return errors.New("autonomous.automation.repo_ci_watch.security_workflows: workflow names must be non-empty")
		}
	}
	return nil
}
