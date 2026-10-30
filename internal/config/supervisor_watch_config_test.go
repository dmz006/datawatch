package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSupervisorWatchConfigLoads — the supervisor watch block's config-
// load path: a mode other than off|shadow|on or a negative minute
// threshold is refused when the config is loaded (the write paths refuse
// the same set), and every legal value (plus an unset block) loads clean.
func TestSupervisorWatchConfigLoads(t *testing.T) {
	load := func(t *testing.T, body string) (*Config, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config file: %v", err)
		}
		return Load(path)
	}

	// Unset block: loads clean, the mode reads shadow (the release
	// default — reports only, never acts) via the accessor, and the unset
	// minute thresholds read their one defaults.
	cfg, err := load(t, "agent:\n  enabled: true\n")
	if err != nil {
		t.Fatalf("Load (unset supervisor_watch): %v", err)
	}
	w := cfg.Autonomous.Automation.SupervisorWatch
	if got := w.EffectiveMode(); got != SupervisorWatchModeShadow {
		t.Errorf("unset block: effective mode = %q, want %q", got, SupervisorWatchModeShadow)
	}
	if got := w.EffectiveAlertMinutes(); got != DefaultAutomationTriageAlertMinutes {
		t.Errorf("unset block: effective alert_minutes = %d, want the shared %d default", got, DefaultAutomationTriageAlertMinutes)
	}
	if got := w.EffectiveSilentMinutes(); got != DefaultSupervisorWatchSilentMinutes {
		t.Errorf("unset block: effective silent_minutes = %d, want the %d default", got, DefaultSupervisorWatchSilentMinutes)
	}

	// Every legal value loads clean, and an explicit 0 (the operator said
	// zero) is kept, not defaulted over.
	for _, mode := range []string{SupervisorWatchModeOff, SupervisorWatchModeShadow, SupervisorWatchModeOn} {
		cfg, err := load(t, "autonomous:\n  automation:\n    supervisor_watch:\n      mode: "+mode+"\n")
		if err != nil {
			t.Errorf("Load (mode: %s): %v, want accepted", mode, err)
			continue
		}
		if got := cfg.Autonomous.Automation.SupervisorWatch.EffectiveMode(); got != mode {
			t.Errorf("Load (mode: %s): effective = %q, want %q", mode, got, mode)
		}
	}
	cfg, err = load(t, "autonomous:\n  automation:\n    supervisor_watch:\n      alert_minutes: 0\n      silent_minutes: 0\n")
	if err != nil {
		t.Fatalf("Load (both thresholds 0): %v, want accepted", err)
	}
	if got := cfg.Autonomous.Automation.SupervisorWatch.EffectiveAlertMinutes(); got != 0 {
		t.Errorf("Load (alert_minutes: 0): effective = %d, want 0 (kept, not defaulted over)", got)
	}
	if got := cfg.Autonomous.Automation.SupervisorWatch.EffectiveSilentMinutes(); got != 0 {
		t.Errorf("Load (silent_minutes: 0): effective = %d, want 0 (kept, not defaulted over)", got)
	}
	// An explicit override is kept too.
	cfg, err = load(t, "autonomous:\n  automation:\n    supervisor_watch:\n      alert_minutes: 7\n      silent_minutes: 45\n")
	if err != nil {
		t.Fatalf("Load (alert_minutes: 7, silent_minutes: 45): %v, want accepted", err)
	}
	if got := cfg.Autonomous.Automation.SupervisorWatch.EffectiveAlertMinutes(); got != 7 {
		t.Errorf("Load (alert_minutes: 7): effective = %d, want 7", got)
	}
	if got := cfg.Autonomous.Automation.SupervisorWatch.EffectiveSilentMinutes(); got != 45 {
		t.Errorf("Load (silent_minutes: 45): effective = %d, want 45", got)
	}

	// Any bad mode is refused on config load, naming the key.
	for _, mode := range []string{"loud", "enabled", "onoff", "ONSHADOW"} {
		_, err := load(t, "autonomous:\n  automation:\n    supervisor_watch:\n      mode: "+mode+"\n")
		if err == nil {
			t.Errorf("Load (mode: %s): accepted, want refused", mode)
			continue
		}
		if !strings.Contains(err.Error(), "supervisor_watch") {
			t.Errorf("Load (mode: %s): error %q does not name the supervisor_watch key", mode, err)
		}
	}

	// A negative threshold is refused on config load too.
	for _, key := range []string{"alert_minutes", "silent_minutes"} {
		_, err := load(t, "autonomous:\n  automation:\n    supervisor_watch:\n      "+key+": -1\n")
		if err == nil {
			t.Errorf("Load (%s: -1): accepted, want refused", key)
			continue
		}
		if !strings.Contains(err.Error(), key) {
			t.Errorf("Load (%s: -1): error %q does not name the %s key", key, err, key)
		}
	}
}

// TestSupervisorWatchConfigPollSecondsLoads — the poll_seconds config-load
// path: unset reads the 60-second default, a legal override is kept, and a
// negative value or one below the 60-second minimum is refused (the write
// paths refuse the same set).
func TestSupervisorWatchConfigPollSecondsLoads(t *testing.T) {
	load := func(t *testing.T, body string) (*Config, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config file: %v", err)
		}
		return Load(path)
	}

	// Unset: loads clean, the poll interval reads the 60-second default.
	cfg, err := load(t, "agent:\n  enabled: true\n")
	if err != nil {
		t.Fatalf("Load (unset supervisor_watch): %v", err)
	}
	if got, want := cfg.Autonomous.Automation.SupervisorWatch.EffectivePollSeconds(), DefaultSupervisorWatchPollSeconds; got != want {
		t.Errorf("unset block: effective poll_seconds = %d, want the %d default", got, want)
	}

	// A legal override (>= the minimum) is kept, and the minimum itself loads.
	for _, v := range []int{SupervisorWatchMinPollSeconds, 120, 300} {
		body := "autonomous:\n  automation:\n    supervisor_watch:\n      poll_seconds: " + itoa(v) + "\n"
		cfg, err := load(t, body)
		if err != nil {
			t.Errorf("Load (poll_seconds: %d): %v, want accepted", v, err)
			continue
		}
		if got := cfg.Autonomous.Automation.SupervisorWatch.EffectivePollSeconds(); got != v {
			t.Errorf("Load (poll_seconds: %d): effective = %d, want %d", v, got, v)
		}
	}

	// A negative value or one below the minimum is refused on config load,
	// each naming the poll_seconds key.
	for _, v := range []int{-1, 1, 59} {
		body := "autonomous:\n  automation:\n    supervisor_watch:\n      poll_seconds: " + itoa(v) + "\n"
		_, err := load(t, body)
		if err == nil {
			t.Errorf("Load (poll_seconds: %d): accepted, want refused", v)
			continue
		}
		if !strings.Contains(err.Error(), "poll_seconds") {
			t.Errorf("Load (poll_seconds: %d): error %q does not name the poll_seconds key", v, err)
		}
	}
}

func itoa(v int) string { return fmt.Sprintf("%d", v) }
