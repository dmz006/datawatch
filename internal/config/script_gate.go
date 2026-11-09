// Package config — script gate timeout setting.
//
// autonomous.script_gate_timeout_seconds caps how long a script gate may run
// before it is stopped. The default is 900 seconds, 0 means no cap, and a
// negative value is refused when the configuration is validated.
package config

import (
	"fmt"
	"strings"
	"time"
)

// ProjectGateCommandMaxBytes is the longest project gate command, in bytes,
// after trimming surrounding whitespace.
const ProjectGateCommandMaxBytes = 1024

// ValidateProjectGateCommand refuses a project gate command that cannot be
// stored or run as one line of shell text. The value is trimmed first; an
// empty result means unset and is valid. The error names keyPath plus
// ".project_gate_command", so the caller's setting path (for example
// "autonomous" or "projects.<name>.policy") appears in the message.
func ValidateProjectGateCommand(keyPath, value string) error {
	v := strings.TrimSpace(value)
	if len(v) > ProjectGateCommandMaxBytes {
		return fmt.Errorf("%s.project_gate_command: %d bytes is too long; at most %d", keyPath, len(v), ProjectGateCommandMaxBytes)
	}
	if strings.ContainsRune(v, 0) {
		return fmt.Errorf("%s.project_gate_command: must not contain a NUL byte", keyPath)
	}
	if strings.ContainsAny(v, "\n\r") {
		return fmt.Errorf("%s.project_gate_command: must be a single line; remove the newline", keyPath)
	}
	return nil
}

// EffectiveProjectGateCommand returns the project gate command for one
// project: the project policy's value when the policy sets the key (an
// explicit empty string unsets it for that project), otherwise the daemon
// autonomous.project_gate_command. A nil receiver or policy is allowed. The
// result is trimmed, and "" means unset.
func (a *AutonomousConfig) EffectiveProjectGateCommand(policy *ProjectPolicy) string {
	if policy != nil && policy.ProjectGateCommand != nil {
		return strings.TrimSpace(*policy.ProjectGateCommand)
	}
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.ProjectGateCommand)
}

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
