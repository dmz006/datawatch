package config

import "strings"

// Values of autonomous.restart_policy: what a requested daemon restart does
// about work that is running.
const (
	// RestartPolicyImmediate restarts at once (the default, and the behaviour
	// before the setting existed).
	RestartPolicyImmediate = "immediate"
	// RestartPolicyWaitQuiet waits up to autonomous.restart_quiet_timeout_seconds
	// for zero running tasks, then proceeds or aborts per restart_on_timeout.
	RestartPolicyWaitQuiet = "wait_quiet"
	// RestartPolicyDrain stops new tasks from starting, lets running ones
	// finish, then restarts.
	RestartPolicyDrain = "drain"
)

// Values of autonomous.restart_on_timeout.
const (
	RestartOnTimeoutProceed = "proceed"
	RestartOnTimeoutAbort   = "abort"
)

// DefaultRestartQuietTimeoutSeconds is how long wait_quiet waits when
// autonomous.restart_quiet_timeout_seconds is 0 or unset.
const DefaultRestartQuietTimeoutSeconds = 600

// NormalizeRestartPolicy returns the effective policy; unset or unrecognised
// reads as immediate.
func NormalizeRestartPolicy(v string) string {
	switch s := strings.ToLower(strings.TrimSpace(v)); s {
	case RestartPolicyWaitQuiet, RestartPolicyDrain:
		return s
	}
	return RestartPolicyImmediate
}

// ValidRestartPolicy reports whether v is a recognised policy value.
func ValidRestartPolicy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case RestartPolicyImmediate, RestartPolicyWaitQuiet, RestartPolicyDrain:
		return true
	}
	return false
}

// NormalizeRestartOnTimeout returns "abort" or "proceed"; unset or
// unrecognised reads as proceed.
func NormalizeRestartOnTimeout(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), RestartOnTimeoutAbort) {
		return RestartOnTimeoutAbort
	}
	return RestartOnTimeoutProceed
}

// ValidRestartOnTimeout reports whether v is "proceed" or "abort".
func ValidRestartOnTimeout(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case RestartOnTimeoutProceed, RestartOnTimeoutAbort:
		return true
	}
	return false
}

// NormalizeRestartQuietTimeoutSeconds returns the effective wait: 0 or a
// negative value reads as the default.
func NormalizeRestartQuietTimeoutSeconds(v int) int {
	if v <= 0 {
		return DefaultRestartQuietTimeoutSeconds
	}
	return v
}
