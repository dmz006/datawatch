package config

import "strings"

// VerifierInfraEscalationThresholdDefault is the number of consecutive infra
// verifier failures on one task after which the next verification runs on the
// fallback backend, used when autonomous.verifier_infra_escalation_threshold
// is unset or not positive.
const VerifierInfraEscalationThresholdDefault = 2

// FallbackVerificationBackendDefault is the fallback verification backend used
// when autonomous.fallback_verification_backend is empty.
const FallbackVerificationBackendDefault = "claude-code"

// NormalizeVerifierInfraEscalationThreshold returns the effective escalation
// threshold: the configured value when positive, otherwise the default.
func NormalizeVerifierInfraEscalationThreshold(v int) int {
	if v <= 0 {
		return VerifierInfraEscalationThresholdDefault
	}
	return v
}

// NormalizeFallbackVerificationBackend returns the effective fallback
// verification backend: the trimmed configured value, or the default when empty.
func NormalizeFallbackVerificationBackend(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return FallbackVerificationBackendDefault
	}
	return v
}
