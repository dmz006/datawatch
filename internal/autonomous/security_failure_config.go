package autonomous

import (
	"fmt"
	"strings"
)

// Security failure (autonomous.automation.security_failure) — the block
// that switches the security-failure handling on, in shadow, or off.
// Every default is applied in exactly one place: the Effective* accessors
// in this file. No call site may hard-code a default; callers read the
// effective value.

// Security failure modes — the legal modes. An empty (unset) mode reads
// SecurityFailureModeShadow, which is the first-release default: the
// feature is switched on in shadow mode first, with no behaviour change.
// Any other value is refused on every write path (REST PUT and config
// load reject it before it is stored).
const (
	SecurityFailureModeOff    = "off"
	SecurityFailureModeShadow = "shadow"
	SecurityFailureModeOn     = "on"
)

// SecurityFailureConfig — the security failure knob
// (autonomous.automation.security_failure.*).
//
//   - Mode: off, shadow or on. An empty (unset) mode reads
//     SecurityFailureModeShadow (the first-release default — shadow
//     first, no behaviour change). Any other value is refused on every
//     write path.
//
// There is no other key: the security workflow set is read from
// autonomous.automation.repo_ci_watch.security_workflows, and the alert
// time is the existing autonomous.automation.triage_alert_minutes.
type SecurityFailureConfig struct {
	Mode string `yaml:"mode" json:"mode"`
}

// EffectiveMode returns the mode in effect: an empty (unset) value reads
// SecurityFailureModeShadow (the first-release default). Any value other
// than off, shadow or on is refused on the write paths, so this never
// has to guess for it. This accessor is the only place the shadow
// default is applied; a caller never writes the literal.
func (c SecurityFailureConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return SecurityFailureModeShadow
	}
	return mode
}

// DefaultSecurityFailureConfig returns a SecurityFailureConfig fully
// populated with the default every Effective* accessor applies (shadow,
// the first-release default), so callers can start from a known-correct
// value without remembering any default.
func DefaultSecurityFailureConfig() SecurityFailureConfig {
	return SecurityFailureConfig{Mode: SecurityFailureModeShadow}
}

// ValidateSecurityFailureMode accepts the three legal modes (off, shadow,
// on; case-insensitive, trimmed; empty = unset = shadow) and rejects
// every other value with a clear error naming the accepted set.
func ValidateSecurityFailureMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SecurityFailureModeOff, SecurityFailureModeShadow, SecurityFailureModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.security_failure.mode: %q is not a valid value (must be %q, %q or %q)",
			mode, SecurityFailureModeOff, SecurityFailureModeShadow, SecurityFailureModeOn)
	}
}

// ValidateSecurityFailureConfig checks a whole SecurityFailureConfig the
// same way the write paths do. Every write path runs these same rules, so
// a bad mode is refused consistently rather than normalized silently.
func ValidateSecurityFailureConfig(c SecurityFailureConfig) error {
	return ValidateSecurityFailureMode(c.Mode)
}
