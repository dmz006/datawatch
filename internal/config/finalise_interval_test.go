package config

import (
	"os"
	"path/filepath"
	"testing"
)

// loadSessionYAML writes body to a temporary config file and loads it.
func loadSessionYAML(t *testing.T, body string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestFinaliseIntervalDefaultIsTen(t *testing.T) {
	if got := DefaultConfig().Session.FinaliseIntervalMinutes; got != 10 {
		t.Fatalf("DefaultConfig finalise_interval_minutes=%d, want 10", got)
	}
	cfg := loadSessionYAML(t, "session:\n  max_sessions: 5\n")
	if got := cfg.Session.FinaliseIntervalMinutes; got != 10 {
		t.Fatalf("config without the key loaded as %d, want 10", got)
	}
}

func TestFinaliseIntervalExplicitValueKept(t *testing.T) {
	cfg := loadSessionYAML(t, "session:\n  finalise_interval_minutes: 25\n")
	if got := cfg.Session.FinaliseIntervalMinutes; got != 25 {
		t.Fatalf("finalise_interval_minutes=%d, want 25", got)
	}
}

func TestFinaliseIntervalZeroDisables(t *testing.T) {
	cfg := loadSessionYAML(t, "session:\n  finalise_interval_minutes: 0\n")
	if got := cfg.Session.FinaliseIntervalMinutes; got != 0 {
		t.Fatalf("explicit 0 loaded as %d, want 0", got)
	}
}

func TestFinaliseIntervalNegativeUsesDefault(t *testing.T) {
	cfg := loadSessionYAML(t, "session:\n  finalise_interval_minutes: -5\n")
	if got := cfg.Session.FinaliseIntervalMinutes; got != 10 {
		t.Fatalf("negative value loaded as %d, want 10", got)
	}
}

func TestFinaliseIntervalYAMLRoundTrip(t *testing.T) {
	for _, want := range []int{0, 7, 10} {
		cfg := DefaultConfig()
		cfg.Session.FinaliseIntervalMinutes = want
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := Save(cfg, path); err != nil {
			t.Fatalf("save: %v", err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := loaded.Session.FinaliseIntervalMinutes; got != want {
			t.Fatalf("round trip of %d gave %d", want, got)
		}
	}
}
