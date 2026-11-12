// Normalization for the autonomous.usage_backoff and
// autonomous.backoff_max_wait_minutes keys (BL428 Phase 4): per-backend
// back-off after a rate/usage limit. Invalid values degrade safely and log
// a warning rather than failing daemon start.
package config

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

const (
	// UsageBackoffPauseBackend holds new work for a rate-limited backend.
	UsageBackoffPauseBackend = "pause_backend"
	// UsageBackoffOff disables automatic back-off.
	UsageBackoffOff = "off"
	// BackoffMaxWaitDefaultMinutes is the back-off applied when a rate limit
	// carries no reset time.
	BackoffMaxWaitDefaultMinutes = 60
)

var usageBackoffWarnOnce sync.Once

// normalizeUsageBackoff maps "" to the default and any unrecognized value to
// "off", warning once.
func normalizeUsageBackoff(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return UsageBackoffPauseBackend
	case UsageBackoffPauseBackend:
		return UsageBackoffPauseBackend
	case UsageBackoffOff:
		return UsageBackoffOff
	}
	usageBackoffWarnOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "[warn] autonomous.usage_backoff: unrecognized value %q — treating as %q (valid: %q, %q)\n",
			v, UsageBackoffOff, UsageBackoffPauseBackend, UsageBackoffOff)
	})
	return UsageBackoffOff
}

// normalizeBackoffMaxWaitMinutes returns v, or the default when v < 1 (a
// warning is logged for an explicit negative/zero-but-set value only when
// it is below zero; 0 is simply "unset").
func normalizeBackoffMaxWaitMinutes(v int) int {
	if v >= 1 {
		return v
	}
	if v < 0 {
		fmt.Fprintf(os.Stderr, "[warn] autonomous.backoff_max_wait_minutes: %d < 1 — clamping to %d\n", v, BackoffMaxWaitDefaultMinutes)
	}
	return BackoffMaxWaitDefaultMinutes
}

// ValidateUsageBackoff reports an error for a value that is neither empty
// (the default) nor a recognized mode. Surfaces that accept the key from an
// operator call it so a typo is a 400 rather than a silent "off".
func ValidateUsageBackoff(v string) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", UsageBackoffPauseBackend, UsageBackoffOff:
		return nil
	}
	return fmt.Errorf("invalid usage_backoff %q: must be %q or %q", v, UsageBackoffPauseBackend, UsageBackoffOff)
}

// EffectiveUsageBackoff returns the mode and maximum back-off minutes a
// reader should act on, applying the same defaults as config load.
func EffectiveUsageBackoff(mode string, maxWaitMinutes int) (string, int) {
	return normalizeUsageBackoff(mode), normalizeBackoffMaxWaitMinutes(maxWaitMinutes)
}
