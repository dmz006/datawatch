package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecurityFailureConfigLoads — the security failure block's config-
// load path: a mode other than off|shadow|on is refused when the config
// is loaded (the write path refuses the same set), and every legal value
// (plus an unset block) loads clean.
func TestSecurityFailureConfigLoads(t *testing.T) {
	load := func(t *testing.T, body string) (*Config, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config file: %v", err)
		}
		return Load(path)
	}

	// Unset block: loads clean, and the mode reads shadow (the
	// first-release default) via the accessor.
	cfg, err := load(t, "agent:\n  enabled: true\n")
	if err != nil {
		t.Fatalf("Load (unset security_failure): %v", err)
	}
	if got := cfg.Autonomous.Automation.SecurityFailure.EffectiveMode(); got != SecurityFailureModeShadow {
		t.Errorf("unset block: effective mode = %q, want %q", got, SecurityFailureModeShadow)
	}

	// Every legal value loads clean.
	for _, mode := range []string{SecurityFailureModeOff, SecurityFailureModeShadow, SecurityFailureModeOn} {
		cfg, err := load(t, "autonomous:\n  automation:\n    security_failure:\n      mode: "+mode+"\n")
		if err != nil {
			t.Errorf("Load (mode: %s): %v, want accepted", mode, err)
			continue
		}
		if got := cfg.Autonomous.Automation.SecurityFailure.EffectiveMode(); got != mode {
			t.Errorf("Load (mode: %s): effective = %q, want %q", mode, got, mode)
		}
	}

	// Any other value is refused on config load, naming the key.
	for _, mode := range []string{"loud", "enabled", "onoff", "ONSHADOW"} {
		_, err := load(t, "autonomous:\n  automation:\n    security_failure:\n      mode: "+mode+"\n")
		if err == nil {
			t.Errorf("Load (mode: %s): accepted, want refused", mode)
			continue
		}
		if !strings.Contains(err.Error(), "security_failure") {
			t.Errorf("Load (mode: %s): error %q does not name the security_failure key", mode, err)
		}
	}
}
