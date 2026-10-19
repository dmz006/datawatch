package config

// SessionIdleTimeoutDefaultSeconds is the default idle bound on one autonomous
// worker or verifier session, used when autonomous.session_idle_timeout_seconds
// is unset or not positive.
const SessionIdleTimeoutDefaultSeconds = 900

// NormalizeSessionIdleTimeoutSeconds returns the effective session idle timeout
// in seconds: the configured value when positive, otherwise
// SessionIdleTimeoutDefaultSeconds.
func NormalizeSessionIdleTimeoutSeconds(v int) int {
	if v <= 0 {
		return SessionIdleTimeoutDefaultSeconds
	}
	return v
}
