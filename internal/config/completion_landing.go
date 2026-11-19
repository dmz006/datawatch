// Completion landing gate settings (autonomous.completion_landing).
package config

// CompletionLandingConfig holds the autonomous.completion_landing keys. The
// gate refuses a change to completed unless the Automaton's branch is merged
// into the default branch or the record carries an explicit no-code reason.
// Enabled is a pointer so an explicit false survives a YAML/JSON round trip.
type CompletionLandingConfig struct {
	// Enabled turns the gate on or off. Absent reads true.
	Enabled *bool `yaml:"enabled" json:"enabled"`
}

// DefaultCompletionLandingEnabled is the safe default: the gate is on.
const DefaultCompletionLandingEnabled = true

// EffectiveEnabled reports whether the gate is on. Absent is on.
func (c CompletionLandingConfig) EffectiveEnabled() bool {
	if c.Enabled == nil {
		return DefaultCompletionLandingEnabled
	}
	return *c.Enabled
}
