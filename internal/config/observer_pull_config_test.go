package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserverPullConfig_DefaultsAreOff(t *testing.T) {
	d := DefaultObserverPullConfig()
	if d.Enabled {
		t.Errorf("DefaultObserverPullConfig().Enabled = %v, want false (off by default)", d.Enabled)
	}
	if d.DefaultIntervalS != 30 {
		t.Errorf("DefaultObserverPullConfig().DefaultIntervalS = %d, want 30", d.DefaultIntervalS)
	}
	if d.DefaultTimeoutS != 5 {
		t.Errorf("DefaultObserverPullConfig().DefaultTimeoutS = %d, want 5", d.DefaultTimeoutS)
	}

	// The shipped Config default must resolve an absent observer.pull block
	// to the same value (Enabled false, 30/5) rather than a zero struct.
	cfg := DefaultConfig()
	if cfg.Observer.Pull.Enabled {
		t.Errorf("DefaultConfig().Observer.Pull.Enabled = %v, want false", cfg.Observer.Pull.Enabled)
	}
	if cfg.Observer.Pull.DefaultIntervalS != 30 {
		t.Errorf("DefaultConfig().Observer.Pull.DefaultIntervalS = %d, want 30", cfg.Observer.Pull.DefaultIntervalS)
	}
	if cfg.Observer.Pull.DefaultTimeoutS != 5 {
		t.Errorf("DefaultConfig().Observer.Pull.DefaultTimeoutS = %d, want 5", cfg.Observer.Pull.DefaultTimeoutS)
	}
	if err := ValidateObserverPullConfig(cfg.Observer.Pull); err != nil {
		t.Errorf("DefaultConfig().Observer.Pull must validate: %v", err)
	}
}

func TestObserverPullConfig_ValidateRejectsSubFloorInterval(t *testing.T) {
	for _, interval := range []int{0, 1, 4} {
		cfg := ObserverPullConfig{Enabled: false, DefaultIntervalS: interval, DefaultTimeoutS: 5}
		err := ValidateObserverPullConfig(cfg)
		if err == nil {
			t.Fatalf("interval %d: expected error below the 5s floor, got nil", interval)
		}
		if !strings.Contains(err.Error(), "observer.pull.default_interval_s") {
			t.Errorf("interval %d: error must name the key observer.pull.default_interval_s, got %q", interval, err)
		}
	}
}

func TestObserverPullConfig_ValidateRejectsNegativeValues(t *testing.T) {
	err := ValidateObserverPullConfig(ObserverPullConfig{DefaultIntervalS: -1, DefaultTimeoutS: 5})
	if err == nil {
		t.Fatalf("negative interval: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "observer.pull.default_interval_s") {
		t.Errorf("negative interval: error must name observer.pull.default_interval_s, got %q", err)
	}

	err = ValidateObserverPullConfig(ObserverPullConfig{DefaultIntervalS: 30, DefaultTimeoutS: -3})
	if err == nil {
		t.Fatalf("negative timeout: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "observer.pull.default_timeout_s") {
		t.Errorf("negative timeout: error must name observer.pull.default_timeout_s, got %q", err)
	}
}

func TestObserverPullConfig_LoadAppliesDefaultsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := DefaultObserverPullConfig()
	if cfg.Observer.Pull != want {
		t.Errorf("absent observer.pull: got %+v, want %+v (shipped defaults, off)", cfg.Observer.Pull, want)
	}
}

func TestObserverPullConfig_LoadRejectsBadValues(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantKey string
	}{
		{
			name:    "interval below floor",
			yaml:    "observer:\n  pull:\n    enabled: true\n    default_interval_s: 4\n",
			wantKey: "observer.pull.default_interval_s",
		},
		{
			name:    "negative interval",
			yaml:    "observer:\n  pull:\n    default_interval_s: -5\n",
			wantKey: "observer.pull.default_interval_s",
		},
		{
			name:    "negative timeout",
			yaml:    "observer:\n  pull:\n    default_timeout_s: -1\n",
			wantKey: "observer.pull.default_timeout_s",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatalf("expected Load to reject %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("error must name %s, got %q", tc.wantKey, err)
			}
		})
	}
}

func TestObserverPullConfig_LoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "observer:\n" +
		"  pull:\n" +
		"    enabled: true\n" +
		"    default_interval_s: 45\n" +
		"    default_timeout_s: 9\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Observer.Pull.Enabled {
		t.Errorf("Enabled = %v, want true", cfg.Observer.Pull.Enabled)
	}
	if cfg.Observer.Pull.DefaultIntervalS != 45 {
		t.Errorf("DefaultIntervalS = %d, want 45", cfg.Observer.Pull.DefaultIntervalS)
	}
	if cfg.Observer.Pull.DefaultTimeoutS != 9 {
		t.Errorf("DefaultTimeoutS = %d, want 9", cfg.Observer.Pull.DefaultTimeoutS)
	}

	// A sibling observer key must survive alongside pull.
	yaml2 := "observer:\n  conn_correlator: true\n  pull:\n    enabled: true\n    default_interval_s: 10\n"
	path2 := filepath.Join(dir, "config2.yaml")
	if err := os.WriteFile(path2, []byte(yaml2), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg2, err := Load(path2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg2.Observer.ConnCorrelator {
		t.Errorf("ConnCorrelator = %v, want true (sibling key must survive)", cfg2.Observer.ConnCorrelator)
	}
	if !cfg2.Observer.Pull.Enabled || cfg2.Observer.Pull.DefaultIntervalS != 10 {
		t.Errorf("Pull = %+v, want {Enabled:true DefaultIntervalS:10 ...}", cfg2.Observer.Pull)
	}
	// Unset default_timeout_s must keep the shipped 5s default, not zero.
	if cfg2.Observer.Pull.DefaultTimeoutS != 5 {
		t.Errorf("unspecified DefaultTimeoutS = %d, want shipped default 5", cfg2.Observer.Pull.DefaultTimeoutS)
	}
}
