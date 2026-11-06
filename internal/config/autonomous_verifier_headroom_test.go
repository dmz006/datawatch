package config

import (
	"strings"
	"testing"
	"time"
)

// TestVerifierHeadroomKeys_Defaults pins the defaults the two gates read:
// an unset floor is 1024 MB (the compute default, not a second constant), an
// unset or zero freshness bound is 24 hours.
func TestVerifierHeadroomKeys_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.Autonomous.EffectiveVerifierMinFreeMB(); got != 1024 {
		t.Errorf("unset min-free = %d, want 1024", got)
	}
	if got := cfg.Autonomous.EffectiveVerifierHeadroomMaxAge(); got != 24*time.Hour {
		t.Errorf("unset max age = %v, want 24h", got)
	}
	cfg.Autonomous.VerifierHeadroomMaxAgeHours = 0
	if got := cfg.Autonomous.EffectiveVerifierHeadroomMaxAge(); got != 24*time.Hour {
		t.Errorf("explicit 0 max age = %v, want 24h (0 means the default)", got)
	}
}

// TestVerifierHeadroomKeys_YAMLAndExplicitValues checks the YAML keys load:
// an explicit 0 floor is a real zero (check off), not unset; a set age is used.
func TestVerifierHeadroomKeys_YAMLAndExplicitValues(t *testing.T) {
	zero := loadYAMLBody(t, "autonomous:\n  verifier_min_free_mem_mb: 0\n  verifier_headroom_max_age_hours: 48\n")
	if got := zero.Autonomous.EffectiveVerifierMinFreeMB(); got != 0 {
		t.Errorf("explicit 0 floor = %d, want 0 (check off)", got)
	}
	if zero.Autonomous.VerifierMinFreeMemMB == nil {
		t.Error("explicit 0 floor loaded as unset; want a pointer to 0")
	}
	if got := zero.Autonomous.EffectiveVerifierHeadroomMaxAge(); got != 48*time.Hour {
		t.Errorf("max age = %v, want 48h", got)
	}
	set := loadYAMLBody(t, "autonomous:\n  verifier_min_free_mem_mb: 700\n")
	if got := set.Autonomous.EffectiveVerifierMinFreeMB(); got != 700 {
		t.Errorf("floor = %d, want 700", got)
	}
}

// TestVerifierHeadroomKeys_NegativeIsValidationError refuses a negative value
// for either key, naming the key, and accepts the zero sentinels.
func TestVerifierHeadroomKeys_NegativeIsValidationError(t *testing.T) {
	neg := -1
	cfg := DefaultConfig()
	cfg.Autonomous.VerifierMinFreeMemMB = &neg
	if err := validateLoaded(cfg); err == nil || !strings.Contains(err.Error(), "autonomous.verifier_min_free_mem_mb") {
		t.Errorf("negative floor: err = %v, want a verifier_min_free_mem_mb error", err)
	}

	cfg = DefaultConfig()
	cfg.Autonomous.VerifierHeadroomMaxAgeHours = -1
	if err := validateLoaded(cfg); err == nil || !strings.Contains(err.Error(), "autonomous.verifier_headroom_max_age_hours") {
		t.Errorf("negative max age: err = %v, want a verifier_headroom_max_age_hours error", err)
	}

	// The zero sentinels load cleanly through the real loader.
	if got := loadYAMLBody(t, "autonomous:\n  verifier_min_free_mem_mb: 0\n  verifier_headroom_max_age_hours: 0\n"); got.Autonomous.EffectiveVerifierHeadroomMaxAge() != 24*time.Hour {
		t.Errorf("zero max age did not read the 24h default")
	}
}
