// Package config — script gate timeout setting.
//
// autonomous.script_gate_timeout_seconds caps how long a script gate may run
// before it is stopped. The default is 900 seconds, 0 means no cap, and a
// negative value is refused when the configuration is validated.
package config

import (
	"fmt"
	"time"
)

// DefaultScriptGateTimeoutSeconds is the shipped script gate time cap, in
// seconds, used when autonomous.script_gate_timeout_seconds is not set.
const DefaultScriptGateTimeoutSeconds = 900

// EffectiveScriptGateTimeout returns the script gate time cap as a duration.
// A configured value of 0 returns 0, which means no cap.
func (a *AutonomousConfig) EffectiveScriptGateTimeout() time.Duration {
	if a == nil {
		return DefaultScriptGateTimeoutSeconds * time.Second
	}
	if a.ScriptGateTimeoutSeconds == 0 {
		return 0
	}
	return time.Duration(a.ScriptGateTimeoutSeconds) * time.Second
}

// validateScriptGateTimeout refuses a negative script gate time cap. Zero is
// accepted and means no cap.
func validateScriptGateTimeout(seconds int) error {
	if seconds < 0 {
		return fmt.Errorf("autonomous.script_gate_timeout_seconds must be 0 or greater, got %d (0 means no cap)", seconds)
	}
	return nil
}
