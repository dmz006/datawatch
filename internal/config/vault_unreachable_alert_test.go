package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadVaultAlertYAML(t *testing.T, value string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "secrets:\n  vault:\n    unreachable_alert_after_seconds: " + value + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestVaultUnreachableAlertConfigDefault(t *testing.T) {
	if got := DefaultConfig().Secrets.Vault.EffectiveUnreachableAlertAfterSeconds(); got != 120 {
		t.Fatalf("default = %d, want 120", got)
	}
	var unset VaultConfig
	if got := unset.EffectiveUnreachableAlertAfterSeconds(); got != DefaultVaultUnreachableAlertAfterSeconds {
		t.Fatalf("unset = %d, want %d", got, DefaultVaultUnreachableAlertAfterSeconds)
	}
}

func TestVaultUnreachableAlertConfigRoundTrip(t *testing.T) {
	for value, want := range map[string]int{"0": 0, "45": 45} {
		cfg, err := loadVaultAlertYAML(t, value)
		if err != nil {
			t.Fatalf("Load rejected %s: %v", value, err)
		}
		if got := cfg.Secrets.Vault.EffectiveUnreachableAlertAfterSeconds(); got != want {
			t.Fatalf("%s loaded as %d, want %d", value, got, want)
		}
	}
}

func TestVaultUnreachableAlertConfigRejectsNegative(t *testing.T) {
	for _, bad := range []string{"-1", "-120"} {
		if _, err := loadVaultAlertYAML(t, bad); err == nil {
			t.Errorf("Load accepted %s", bad)
		}
	}
}
