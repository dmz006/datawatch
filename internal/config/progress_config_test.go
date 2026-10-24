package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func loadProgressYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestProgressConfigDefaults(t *testing.T) {
	p := DefaultConfig().Autonomous.Progress
	if p.Lanes != 0 || p.InactiveGapHours != 0 || p.Since != "" || p.SnapshotRetentionDays != 0 {
		t.Fatalf("default progress = %+v, want zero numerics and empty since", p)
	}
	if p.BacklogPath != "docs/plans/README.md" {
		t.Fatalf("default backlog_path = %q", p.BacklogPath)
	}
	if p.EffectiveLanes() != 5 || p.EffectiveInactiveGapHours() != 3 || p.EffectiveSnapshotRetentionDays() != 90 {
		t.Fatalf("effective defaults = %d, %v, %d", p.EffectiveLanes(), p.EffectiveInactiveGapHours(), p.EffectiveSnapshotRetentionDays())
	}
	if _, ok := p.SinceTime(); ok {
		t.Fatal("empty since reported a date")
	}
}

func TestProgressConfigLoadFillsDefaults(t *testing.T) {
	cfg, err := loadProgressYAML(t, "autonomous:\n  progress:\n    lanes: 2\n")
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Autonomous.Progress
	if p.Lanes != 2 || p.BacklogPath != "docs/plans/README.md" {
		t.Fatalf("loaded progress = %+v", p)
	}
}

func TestProgressConfigYAMLRoundTrip(t *testing.T) {
	in := AutonomousProgressConfig{
		Lanes:                 8,
		InactiveGapHours:      1.5,
		BacklogPath:           "plans/backlog.md",
		Since:                 "2026-09-01",
		SnapshotRetentionDays: 30,
	}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AutonomousProgressConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip: got %+v, want %+v", out, in)
	}
	cfg, err := loadProgressYAML(t, "autonomous:\n  progress:\n    lanes: 8\n    inactive_gap_hours: 1.5\n    backlog_path: plans/backlog.md\n    since: 2026-09-01\n    snapshot_retention_days: 30\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Autonomous.Progress != in {
		t.Fatalf("loaded %+v, want %+v", cfg.Autonomous.Progress, in)
	}
	if d, ok := cfg.Autonomous.Progress.SinceTime(); !ok || !d.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("SinceTime = %v, %v", d, ok)
	}
}

func TestProgressConfigZeroValues(t *testing.T) {
	// An explicit zero survives a round trip and still resolves to the default.
	in := AutonomousProgressConfig{BacklogPath: "x.md"}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"lanes: 0", "inactive_gap_hours: 0", "snapshot_retention_days: 0"} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("marshalled zero config lacks %q:\n%s", key, raw)
		}
	}
	var zero AutonomousProgressConfig
	if zero.EffectiveLanes() != 5 || zero.EffectiveInactiveGapHours() != 3 ||
		zero.EffectiveSnapshotRetentionDays() != 90 || zero.EffectiveBacklogPath() != "docs/plans/README.md" {
		t.Fatalf("zero value did not resolve to defaults: %+v", zero)
	}
}

func TestProgressConfigResolveBacklogPath(t *testing.T) {
	p := AutonomousProgressConfig{BacklogPath: "plans/README.md"}
	if got := p.ResolveBacklogPath("/repo/base"); got != "/repo/base/plans/README.md" {
		t.Fatalf("with base repo: %q", got)
	}
	wd, _ := os.Getwd()
	if got := p.ResolveBacklogPath(""); got != filepath.Join(wd, "plans/README.md") {
		t.Fatalf("without base repo: %q", got)
	}
	abs := AutonomousProgressConfig{BacklogPath: "/etc/backlog.md"}
	if got := abs.ResolveBacklogPath("/repo/base"); got != "/etc/backlog.md" {
		t.Fatalf("absolute path changed: %q", got)
	}
	var unset AutonomousProgressConfig
	if got := unset.ResolveBacklogPath("/repo/base"); got != "/repo/base/docs/plans/README.md" {
		t.Fatalf("default path: %q", got)
	}
}

func TestProgressConfigValidation(t *testing.T) {
	for _, ok := range []string{"", "2026-10-23", " 2026-10-23 "} {
		if err := ValidateProgressSince(ok); err != nil {
			t.Errorf("ValidateProgressSince(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"yesterday", "2026-13-01", "2026-10-23T10:00:00Z", "23/10/2026", "2026-1-1x"} {
		if err := ValidateProgressSince(bad); err == nil {
			t.Errorf("ValidateProgressSince(%q) accepted", bad)
		}
	}
	if err := ValidateProgressBacklogPath("a\x00b"); err == nil {
		t.Error("NUL byte in backlog_path accepted")
	}
	for _, body := range []string{
		"autonomous:\n  progress:\n    lanes: -1\n",
		"autonomous:\n  progress:\n    inactive_gap_hours: -0.5\n",
		"autonomous:\n  progress:\n    snapshot_retention_days: -3\n",
		"autonomous:\n  progress:\n    since: soon\n",
	} {
		if _, err := loadProgressYAML(t, body); err == nil {
			t.Errorf("Load accepted %q", body)
		}
	}
}
