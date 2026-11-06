package config

import (
	"strings"
	"testing"
	"time"
)

func TestDeployZeroValueValidAndInert(t *testing.T) {
	var c DeployConfig
	if err := c.Validate(); err != nil {
		t.Fatalf("zero value invalid: %v", err)
	}
	if !c.IsZero() {
		t.Fatal("zero value should report IsZero")
	}
	if c.BuildCommand != "" || c.InstallCommand != "" || c.RestartCommand != "" || c.HealthCheck != "" || c.BinaryPath != "" {
		t.Fatal("zero value must set no command")
	}
	if (DeployConfig{BuildCommand: "make"}).IsZero() {
		t.Fatal("set block must not be zero")
	}
}

func TestDeployDurationDefaults(t *testing.T) {
	var c DeployConfig
	if got := c.EffectiveDrainTimeout(); got != 10*time.Minute {
		t.Fatalf("drain default = %v", got)
	}
	if got := c.EffectiveHealthTimeout(); got != 2*time.Minute {
		t.Fatalf("health default = %v", got)
	}
	c = DeployConfig{DrainTimeout: "30s", HealthTimeout: "5s"}
	if c.EffectiveDrainTimeout() != 30*time.Second || c.EffectiveHealthTimeout() != 5*time.Second {
		t.Fatal("explicit durations not honoured")
	}
}

func TestDeployInvalidAndNegativeDurations(t *testing.T) {
	for _, c := range []DeployConfig{
		{DrainTimeout: "soon"},
		{DrainTimeout: "-1m"},
		{HealthTimeout: "abc"},
		{HealthTimeout: "-5s"},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("%+v: want error", c)
		}
	}
}

func TestDeployInstallWithoutBinaryPath(t *testing.T) {
	err := DeployConfig{InstallCommand: "cp a b"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "binary_path") {
		t.Fatalf("want binary_path error, got %v", err)
	}
	if err := (DeployConfig{InstallCommand: "cp a b", BinaryPath: "/usr/local/bin/x"}).Validate(); err != nil {
		t.Fatalf("valid install rejected: %v", err)
	}
}

func TestDeployRelativeBinaryPath(t *testing.T) {
	for _, p := range []string{"bin/datawatch", "./datawatch", "/usr/local/../bin/x", "/usr//bin/x", "/usr/bin/x/"} {
		if err := (DeployConfig{BinaryPath: p}).Validate(); err == nil {
			t.Errorf("binary_path %q: want error", p)
		}
	}
}

func TestDeployUnscopedTmuxKillRefused(t *testing.T) {
	bad := "tmux kill-server"
	for _, c := range []DeployConfig{
		{BuildCommand: bad},
		{InstallCommand: bad, BinaryPath: "/usr/bin/x"},
		{RestartCommand: "systemctl stop x; " + bad},
		{HealthCheck: bad},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("%+v: want refusal", c)
		}
	}
	ok := DeployConfig{RestartCommand: "env -u TMUX -u TMUX_TMPDIR tmux kill-server"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("scoped kill refused: %v", err)
	}
}

func TestDeployCloneIsDeep(t *testing.T) {
	a := DeployConfig{BuildCommand: "make", BinaryPath: "/usr/bin/x", DrainTimeout: "1m"}
	b := a.Clone()
	b.BuildCommand = "other"
	b.DrainTimeout = "2m"
	if a.BuildCommand != "make" || a.DrainTimeout != "1m" {
		t.Fatal("mutating the clone changed the original")
	}
}
