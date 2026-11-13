// Package config — gate drain setting.
//
// autonomous.gate_drain_minutes is how many minutes a graceful restart waits
// for running final gates before it exits. The default is 10, 0 means do not
// wait, and a negative value is refused when the configuration is validated.
// This file only stores and validates the value; the wait is done elsewhere.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultGateDrainMinutes is the shipped gate drain wait, in minutes, used
// when autonomous.gate_drain_minutes is not set.
const DefaultGateDrainMinutes = 10

// NormalizeGateDrainMinutes returns the effective wait in minutes. A negative
// value reads as the default; 0 is kept and means do not wait.
func NormalizeGateDrainMinutes(v int) int {
	if v < 0 {
		return DefaultGateDrainMinutes
	}
	return v
}

// EffectiveGateDrain returns the gate drain wait as a duration. 0 means do
// not wait. A nil receiver returns the default.
func (a *AutonomousConfig) EffectiveGateDrain() time.Duration {
	if a == nil {
		return DefaultGateDrainMinutes * time.Minute
	}
	return time.Duration(NormalizeGateDrainMinutes(a.GateDrainMinutes)) * time.Minute
}

// validateGateDrainMinutes refuses a negative gate drain wait. Zero is
// accepted and means do not wait.
func validateGateDrainMinutes(minutes int) error {
	if minutes < 0 {
		return fmt.Errorf("autonomous.gate_drain_minutes must be 0 or greater, got %d (0 means do not wait)", minutes)
	}
	return nil
}

// ParseGateDrainMinutes validates a candidate autonomous.gate_drain_minutes
// value from a JSON number, an int or text. It must be a whole number >= 0.
func ParseGateDrainMinutes(v any) (int, error) {
	const key = "autonomous.gate_drain_minutes"
	switch x := v.(type) {
	case int:
		if x < 0 {
			return 0, fmt.Errorf("%s: must be a whole number of minutes >= 0 (0 = do not wait), got %d", key, x)
		}
		return x, nil
	case float64:
		if x != float64(int(x)) {
			return 0, fmt.Errorf("%s: must be a whole number of minutes, got %v", key, x)
		}
		return ParseGateDrainMinutes(int(x))
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("%s: must be a whole number of minutes >= 0 (0 = do not wait), got %q", key, x)
		}
		return ParseGateDrainMinutes(n)
	}
	return 0, fmt.Errorf("%s: must be a whole number of minutes >= 0 (0 = do not wait), got %v", key, v)
}
