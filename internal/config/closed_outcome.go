package config

import (
	"fmt"
	"strings"
)

// ValidateClosedOutcomeMode refuses a closed outcome mode other than on, shadow
// or off for the named key. Empty is valid and reads as the key's default.
func ValidateClosedOutcomeMode(key, mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "on", "shadow", "off":
		return nil
	}
	return fmt.Errorf("%s: invalid value %q (valid values: \"on\", \"shadow\", \"off\", or empty for the default)", key, mode)
}

func closedOutcomeMode(v, def string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on":
		return "on"
	case "shadow":
		return "shadow"
	case "off":
		return "off"
	}
	return def
}

// EffectiveClosedOutcomeArchive returns the archive guard's mode: on, shadow or
// off. Empty or unknown reads as on, the default.
func (a AutonomousConfig) EffectiveClosedOutcomeArchive() string {
	return closedOutcomeMode(a.ClosedOutcomeArchive, "on")
}

// EffectiveClosedOutcomeCheck returns the standing check's mode: on, shadow or
// off. Empty or unknown reads as shadow, the default.
func (a AutonomousConfig) EffectiveClosedOutcomeCheck() string {
	return closedOutcomeMode(a.ClosedOutcomeCheck, "shadow")
}
