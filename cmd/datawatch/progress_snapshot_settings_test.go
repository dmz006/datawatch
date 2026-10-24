package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestProgressSnapshotSettingsDefaults(t *testing.T) {
	got := progressSnapshotSettings(&config.Config{})
	if got.RetentionDays != 90 || got.Lanes != 5 || got.GapHours != 3 || !got.Since.IsZero() {
		t.Fatalf("unset config must give the defaults: %+v", got)
	}
	if !filepath.IsAbs(got.BacklogPath) || filepath.Base(got.BacklogPath) != "README.md" {
		t.Fatalf("backlog path must resolve to the default file: %q", got.BacklogPath)
	}
}

func TestProgressSnapshotSettingsFromConfig(t *testing.T) {
	var cfg config.Config
	cfg.Autonomous.WorktreeBaseRepo = "/srv/repo"
	cfg.Autonomous.Progress = config.AutonomousProgressConfig{
		Lanes: 3, InactiveGapHours: 2.5, BacklogPath: "plans/index.md", Since: "2026-10-01", SnapshotRetentionDays: 30,
	}
	got := progressSnapshotSettings(&cfg)
	if got.RetentionDays != 30 || got.Lanes != 3 || got.GapHours != 2.5 {
		t.Fatalf("settings: %+v", got)
	}
	if !got.Since.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("since: %v", got.Since)
	}
	if got.BacklogPath != filepath.Join("/srv/repo", "plans/index.md") {
		t.Fatalf("backlog path resolves against the worktree base repo: %q", got.BacklogPath)
	}
}
