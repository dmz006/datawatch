package config

// DescendantStallDefaultSeconds is the default descendant-stall bound on one
// pi-prompt worker whose only liveness is a work child process, used when
// autonomous.descendant_stall_seconds is unset or not positive.
const DescendantStallDefaultSeconds = 2700

// NormalizeDescendantStallSeconds returns the effective descendant stall bound
// in seconds: the configured value when positive, otherwise
// DescendantStallDefaultSeconds.
func NormalizeDescendantStallSeconds(v int) int {
	if v <= 0 {
		return DescendantStallDefaultSeconds
	}
	return v
}
