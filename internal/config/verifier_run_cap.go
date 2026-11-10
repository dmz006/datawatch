package config

import "fmt"

// VerifierRunCapDefaultSeconds is the default wall-clock cap on one verifier
// session, used when autonomous.verifier_max_run_seconds is left at its
// default. 0 is a valid configured value meaning no cap.
const VerifierRunCapDefaultSeconds = 2700

// ValidateVerifierRunCapSeconds refuses a negative autonomous.verifier_max_run_seconds.
// Zero is accepted and means no cap.
func ValidateVerifierRunCapSeconds(v int) error {
	if v < 0 {
		return fmt.Errorf("autonomous.verifier_max_run_seconds must not be negative, got %d (0 means no cap)", v)
	}
	return nil
}

// VerifierRunCapSeconds returns the effective verifier run cap in seconds.
// The value is returned unchanged: 0 means no cap and is never replaced by the default.
func VerifierRunCapSeconds(v int) int {
	return v
}
