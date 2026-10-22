package config

import "testing"

func TestUsageBackoffDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Autonomous.UsageBackoff != UsageBackoffPauseBackend {
		t.Errorf("default usage_backoff = %q, want %q", cfg.Autonomous.UsageBackoff, UsageBackoffPauseBackend)
	}
	if cfg.Autonomous.BackoffMaxWaitMinutes != 60 {
		t.Errorf("default backoff_max_wait_minutes = %d, want 60", cfg.Autonomous.BackoffMaxWaitMinutes)
	}
}

func TestNormalizeUsageBackoff(t *testing.T) {
	cases := map[string]string{
		"":              UsageBackoffPauseBackend,
		"pause_backend": UsageBackoffPauseBackend,
		"off":           UsageBackoffOff,
		"OFF":           UsageBackoffOff,
		"bogus":         UsageBackoffOff,
	}
	for in, want := range cases {
		if got := normalizeUsageBackoff(in); got != want {
			t.Errorf("normalizeUsageBackoff(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeBackoffMaxWaitMinutes(t *testing.T) {
	cases := map[int]int{-5: 60, 0: 60, 1: 1, 15: 15, 240: 240}
	for in, want := range cases {
		if got := normalizeBackoffMaxWaitMinutes(in); got != want {
			t.Errorf("normalizeBackoffMaxWaitMinutes(%d) = %d, want %d", in, got, want)
		}
	}
}
