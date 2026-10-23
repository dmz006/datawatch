package config

// VerifierNoProgressTimeoutDefaultSeconds is the default no-progress bound on
// one verifier session: how long it may show neither an output file nor pane
// activity before it is ended. Used when
// autonomous.verifier_no_progress_timeout_seconds is unset or not positive.
const VerifierNoProgressTimeoutDefaultSeconds = 600

// NormalizeVerifierNoProgressTimeoutSeconds returns the effective verifier
// no-progress timeout in seconds: the configured value when positive,
// otherwise VerifierNoProgressTimeoutDefaultSeconds.
func NormalizeVerifierNoProgressTimeoutSeconds(v int) int {
	if v <= 0 {
		return VerifierNoProgressTimeoutDefaultSeconds
	}
	return v
}
