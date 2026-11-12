package config

// StuckRunningDefaultSeconds is the default bound of the autonomous
// stuck-running watchdog, used when autonomous.stuck_running_seconds is unset
// or not positive.
const StuckRunningDefaultSeconds = 900

// NormalizeStuckRunningSeconds returns the effective stuck-running bound in
// seconds: the configured value when positive, otherwise
// StuckRunningDefaultSeconds.
func NormalizeStuckRunningSeconds(v int) int {
	if v <= 0 {
		return StuckRunningDefaultSeconds
	}
	return v
}
