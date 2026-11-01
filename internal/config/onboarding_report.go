// Onboarding report config: the onboarding_report block.
//
// The onboarding report tells a user what is configured, what is missing and
// what to try next. This block holds its switches; the report reads them
// through the Effective* accessors so an unset key always reads its default.
// Booleans are *bool so an explicit false survives a config round trip.
package config

// OnboardingReportConfig is the onboarding_report block.
type OnboardingReportConfig struct {
	// Enabled turns the report on. nil reads as true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// IncludeNextSteps adds the ordered "what to try next" suggestions.
	// nil reads as true.
	IncludeNextSteps *bool `yaml:"include_next_steps,omitempty" json:"include_next_steps,omitempty"`
	// DisabledChecks lists check ids left out of the report. An id that
	// names no check is ignored.
	DisabledChecks []string `yaml:"disabled_checks,omitempty" json:"disabled_checks,omitempty"`
}

// IsZero reports whether no key of the block is set, so yaml.v3 leaves the
// block out of the file.
func (c OnboardingReportConfig) IsZero() bool {
	return c.Enabled == nil && c.IncludeNextSteps == nil && len(c.DisabledChecks) == 0
}

// Clone returns a deep copy that shares no slice or pointer with c.
func (c OnboardingReportConfig) Clone() OnboardingReportConfig {
	out := c
	out.Enabled = cloneBoolPtr(c.Enabled)
	out.IncludeNextSteps = cloneBoolPtr(c.IncludeNextSteps)
	if c.DisabledChecks != nil {
		out.DisabledChecks = append([]string{}, c.DisabledChecks...)
	}
	return out
}

// EffectiveEnabled reports whether the report is on (default true).
func (c OnboardingReportConfig) EffectiveEnabled() bool { return boolOrDefault(c.Enabled, true) }

// EffectiveIncludeNextSteps reports whether next-step suggestions are
// included (default true).
func (c OnboardingReportConfig) EffectiveIncludeNextSteps() bool {
	return boolOrDefault(c.IncludeNextSteps, true)
}

// EffectiveDisabledChecks returns the disabled check ids as a copy.
func (c OnboardingReportConfig) EffectiveDisabledChecks() []string {
	return append([]string{}, c.DisabledChecks...)
}
