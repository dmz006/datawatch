package config

import (
	"fmt"
	"strings"
)

// ValidateShippedRefusal refuses a shipped_refusal mode other than on, shadow
// or off. Empty is valid and reads as on.
func ValidateShippedRefusal(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "on", "shadow", "off":
		return nil
	}
	return fmt.Errorf("autonomous.shipped_refusal: invalid value %q (valid values: \"on\", \"shadow\", \"off\", or empty for \"on\")", mode)
}

// EffectiveShippedRefusal returns the shipped-work rule's mode: on, shadow or
// off. Empty or unknown reads as on, the default.
func (a AutonomousConfig) EffectiveShippedRefusal() string {
	switch strings.ToLower(strings.TrimSpace(a.ShippedRefusal)) {
	case "off":
		return "off"
	case "shadow":
		return "shadow"
	}
	return "on"
}
