package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostLoadAlert_Defaults(t *testing.T) {
	var c HostLoadAlertConfig
	if c.EffectiveMode() != "alert" || c.EffectiveMultiple() != 1.5 || c.EffectiveMinutes() != 10 {
		t.Fatalf("zero value = %s/%v/%d, want alert/1.5/10", c.EffectiveMode(), c.EffectiveMultiple(), c.EffectiveMinutes())
	}
	c = HostLoadAlertConfig{Mode: "reap", Multiple: 2, Minutes: 5}
	if c.EffectiveMode() != "reap" || c.EffectiveMultiple() != 2 || c.EffectiveMinutes() != 5 {
		t.Fatalf("explicit values not kept: %+v", c)
	}
	if DefaultConfig().Autonomous.HostLoadAlert.EffectiveMode() != HostLoadAlertModeAlert {
		t.Fatal("default config does not read as alert")
	}
}

func TestHostLoadAlert_Validate(t *testing.T) {
	for _, m := range []string{"", "off", "alert", "reap"} {
		if err := ValidateHostLoadAlertMode(m); err != nil {
			t.Errorf("mode %q rejected: %v", m, err)
		}
	}
	if ValidateHostLoadAlertMode("kill") == nil {
		t.Error("unknown mode accepted")
	}
	if ValidateHostLoadAlertMultiple(0) != nil || ValidateHostLoadAlertMultiple(2.5) != nil {
		t.Error("valid multiple rejected")
	}
	if ValidateHostLoadAlertMultiple(-0.1) == nil {
		t.Error("negative multiple accepted")
	}
	if ValidateHostLoadAlertMinutes(0) != nil || ValidateHostLoadAlertMinutes(30) != nil {
		t.Error("valid minutes rejected")
	}
	if ValidateHostLoadAlertMinutes(-1) == nil {
		t.Error("negative minutes accepted")
	}
}

func TestHostLoadAlert_LoadYAMLAndRefuseBad(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("autonomous:\n  host_load_alert:\n    mode: reap\n    multiple: 2\n    minutes: 12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(good)
	if err != nil {
		t.Fatalf("Load good: %v", err)
	}
	h := cfg.Autonomous.HostLoadAlert
	if h.Mode != "reap" || h.Multiple != 2 || h.Minutes != 12 {
		t.Fatalf("loaded %+v", h)
	}
	for name, body := range map[string]string{
		"mode":     "mode: kill",
		"multiple": "multiple: -1",
		"minutes":  "minutes: -3",
	} {
		bad := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(bad, []byte("autonomous:\n  host_load_alert:\n    "+body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "host_load_alert") {
			t.Errorf("%s: Load error = %v, want a host_load_alert refusal", name, err)
		}
	}
}
