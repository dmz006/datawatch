package config

// SessionStartConfirmDefaultSeconds is the default bound on a claude-code
// autonomous session's start confirmation, used when
// autonomous.session_start_confirm_seconds is unset or not positive.
const SessionStartConfirmDefaultSeconds = 120

// NormalizeSessionStartConfirmSeconds returns the effective session start
// confirm timeout in seconds: the configured value when positive, otherwise
// SessionStartConfirmDefaultSeconds.
func NormalizeSessionStartConfirmSeconds(v int) int {
	if v <= 0 {
		return SessionStartConfirmDefaultSeconds
	}
	return v
}
