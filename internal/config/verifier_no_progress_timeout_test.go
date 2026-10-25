package config

import "testing"

func TestVerifierWatchdogDefaultAppliesWhenUnset(t *testing.T) {
	for _, in := range []int{0, -5} {
		if got := NormalizeVerifierNoProgressTimeoutSeconds(in); got != 900 {
			t.Fatalf("Normalize(%d) = %d, want 900", in, got)
		}
	}
	if got := NormalizeVerifierNoProgressTimeoutSeconds(90); got != 90 {
		t.Fatalf("Normalize(90) = %d", got)
	}
}
