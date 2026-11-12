package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Unset ui.* splash keys must read their defaults: 6 for the plain
// splash, 9 for the new-version splash.
func TestUISplashSettingsDefaultsWhenUnset(t *testing.T) {
	var u UIConfig
	if got := u.SplashMinSecondsEffective(); got != 6 {
		t.Errorf("unset splash_min_seconds: want default 6, got %d", got)
	}
	if got := u.SplashMinSecondsNewVersionEffective(); got != 9 {
		t.Errorf("unset splash_min_seconds_new_version: want default 9, got %d", got)
	}
	if err := u.validate(); err != nil {
		t.Errorf("unset keys must validate cleanly, got %v", err)
	}
}

// An explicit 0 is a real value, distinct from unset: it is kept as 0
// (0 = keep the legacy timing) and is not replaced by the default.
func TestUISplashSettingsExplicitZeroKept(t *testing.T) {
	u := UIConfig{SplashMinSeconds: intPtr(0), SplashMinSecondsNewVersion: intPtr(0)}
	if got := u.SplashMinSecondsEffective(); got != 0 {
		t.Errorf("explicit 0 splash_min_seconds: want 0, got %d", got)
	}
	if got := u.SplashMinSecondsNewVersionEffective(); got != 0 {
		t.Errorf("explicit 0 splash_min_seconds_new_version: want 0, got %d", got)
	}
	if err := u.validate(); err != nil {
		t.Errorf("explicit 0 must validate cleanly (0 = legacy timing), got %v", err)
	}
}

// 60 is the accepted boundary for both splash keys.
func TestUISplashSettingsSixtyAccepted(t *testing.T) {
	u := UIConfig{SplashMinSeconds: intPtr(60), SplashMinSecondsNewVersion: intPtr(60)}
	if got := u.SplashMinSecondsEffective(); got != 60 {
		t.Errorf("want 60, got %d", got)
	}
	if got := u.SplashMinSecondsNewVersionEffective(); got != 60 {
		t.Errorf("want 60, got %d", got)
	}
	if err := u.validate(); err != nil {
		t.Errorf("60 must be accepted, got %v", err)
	}
}

// 61 is outside the accepted 0..60 range; the error must name the key.
func TestUISplashSettingsSixtyOneRejected(t *testing.T) {
	u := UIConfig{SplashMinSeconds: intPtr(61)}
	err := u.validate()
	if err == nil {
		t.Fatal("61 must be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "ui.splash_min_seconds") {
		t.Errorf("error must name the key ui.splash_min_seconds, got %v", err)
	}

	u = UIConfig{SplashMinSecondsNewVersion: intPtr(61)}
	err = u.validate()
	if err == nil {
		t.Fatal("61 must be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "ui.splash_min_seconds_new_version") {
		t.Errorf("error must name the key ui.splash_min_seconds_new_version, got %v", err)
	}
}

// -1 is below the accepted range; the error must name the key.
func TestUISplashSettingsNegativeOneRejected(t *testing.T) {
	u := UIConfig{SplashMinSeconds: intPtr(-1)}
	err := u.validate()
	if err == nil {
		t.Fatal("-1 must be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "ui.splash_min_seconds") {
		t.Errorf("error must name the key ui.splash_min_seconds, got %v", err)
	}

	u = UIConfig{SplashMinSecondsNewVersion: intPtr(-1)}
	err = u.validate()
	if err == nil {
		t.Fatal("-1 must be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "ui.splash_min_seconds_new_version") {
		t.Errorf("error must name the key ui.splash_min_seconds_new_version, got %v", err)
	}
}

// Both ui.* splash keys must round-trip through YAML: an explicit 0 must
// survive (not be reset to the default) and distinct values must come
// back unchanged.
func TestUISplashSettingsBothKeysRoundTripYAML(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ui-splash-roundtrip.yaml")
	initial := "ui:\n  splash_min_seconds: 0\n  splash_min_seconds_new_version: 30\n"
	if err := os.WriteFile(file, []byte(initial), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UI.SplashMinSeconds == nil || *cfg.UI.SplashMinSeconds != 0 {
		t.Fatalf("explicit 0 splash_min_seconds must be kept, got %v", cfg.UI.SplashMinSeconds)
	}
	if cfg.UI.SplashMinSecondsNewVersion == nil || *cfg.UI.SplashMinSecondsNewVersion != 30 {
		t.Fatalf("want 30, got %v", cfg.UI.SplashMinSecondsNewVersion)
	}
	if cfg.UI.SplashMinSecondsEffective() != 0 || cfg.UI.SplashMinSecondsNewVersionEffective() != 30 {
		t.Fatalf("effective values: got %d / %d, want 0 / 30",
			cfg.UI.SplashMinSecondsEffective(), cfg.UI.SplashMinSecondsNewVersionEffective())
	}
	if err := Save(cfg, file); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(file)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again.UI.SplashMinSeconds == nil || *again.UI.SplashMinSeconds != 0 {
		t.Fatalf("after round trip, explicit 0 splash_min_seconds must be kept, got %v", again.UI.SplashMinSeconds)
	}
	if again.UI.SplashMinSecondsNewVersion == nil || *again.UI.SplashMinSecondsNewVersion != 30 {
		t.Fatalf("after round trip, want 30, got %v", again.UI.SplashMinSecondsNewVersion)
	}

	// The plain struct must also round-trip the explicit 0 through a
	// marshal/unmarshal pair.
	var out []byte
	out, err = yaml.Marshal(again.UI)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var u UIConfig
	if err := yaml.Unmarshal(out, &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.SplashMinSeconds == nil || *u.SplashMinSeconds != 0 {
		t.Fatalf("yaml round trip lost the explicit 0: %v", u.SplashMinSeconds)
	}
}
