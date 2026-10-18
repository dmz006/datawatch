package config

// VerificationTimeoutDefaultSeconds is the default bound on one verifier or
// guardrail one-shot judgment session, used when
// autonomous.verification_timeout_seconds is unset or not positive.
const VerificationTimeoutDefaultSeconds = 1800

// NormalizeVerificationTimeoutSeconds returns the effective verifier/guardrail
// timeout in seconds: the configured value when positive, otherwise
// VerificationTimeoutDefaultSeconds.
func NormalizeVerificationTimeoutSeconds(v int) int {
	if v <= 0 {
		return VerificationTimeoutDefaultSeconds
	}
	return v
}
