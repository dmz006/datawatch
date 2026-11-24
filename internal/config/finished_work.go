package config

import (
	"fmt"
	"strings"
)

// ValidateFinishedWork refuses a finished_work mode other than on, shadow or
// off. Empty is valid and reads as on.
func ValidateFinishedWork(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "on", "shadow", "off":
		return nil
	}
	return fmt.Errorf("autonomous.finished_work: invalid value %q (valid values: \"on\", \"shadow\", \"off\", or empty for \"on\")", mode)
}

// EffectiveFinishedWork returns the finished-work guard's mode: on, shadow or
// off. Empty or unknown reads as on, the default.
func (a AutonomousConfig) EffectiveFinishedWork() string {
	switch strings.ToLower(strings.TrimSpace(a.FinishedWork)) {
	case "off":
		return "off"
	case "shadow":
		return "shadow"
	}
	return "on"
}
