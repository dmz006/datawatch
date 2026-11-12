package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestStatePathStartupDiscussionMigration pins where the startup discussion
// WAL migration looks, given a configured data directory and a decoy HOME:
// the discussions folder must live under the (expanded) data directory, never
// under HOME, and the value used when the data directory is empty must still
// resolve from the config default.
func TestStatePathStartupDiscussionMigration(t *testing.T) {
	// Default resolution must be captured before the decoy HOME is in place,
	// so we compare against the real config default, not the decoy.
	defCfg := config.DefaultConfig()
	if defCfg.DataDir == "" {
		t.Fatal("config default DataDir is empty; applyDefaults failed to supply one")
	}
	wantDefault := filepath.Join(defCfg.DataDir, "discussions")

	tmp := t.TempDir()
	decoy := t.TempDir()
	t.Setenv("HOME", decoy)
	t.Setenv("USERPROFILE", decoy)

	// Configured data directory: resolves under the data directory.
	got := startupDiscussionsDir(tmp)
	if !strings.HasPrefix(got, filepath.Clean(tmp)+string(filepath.Separator)) {
		t.Errorf("startupDiscussionsDir(%q) = %q; want prefix %q/discussions", tmp, got, filepath.Clean(tmp))
	}
	if strings.HasPrefix(got, decoy+string(filepath.Separator)) {
		t.Errorf("startupDiscussionsDir(%q) = %q; must not resolve under decoy HOME %q", tmp, got, decoy)
	}
	if want := filepath.Join(tmp, "discussions"); got != want {
		t.Errorf("startupDiscussionsDir(%q) = %q; want %q", tmp, got, want)
	}

	// Tilde data directory: must expand under HOME (the decoy).
	tgot := startupDiscussionsDir("~/custom-data")
	if want := filepath.Join(decoy, "custom-data", "discussions"); tgot != want {
		t.Errorf("startupDiscussionsDir(~/custom-data) = %q; want %q", tgot, want)
	}

	// Empty data directory: value used falls back to the config default,
	// independent of the (decoy) HOME in effect now.
	dataDir := ""
	if dataDir == "" {
		dataDir = defCfg.DataDir
	}
	if gotDef := startupDiscussionsDir(dataDir); gotDef != wantDefault {
		t.Errorf("startupDiscussionsDir(empty -> default %q) = %q; want %q", defCfg.DataDir, gotDef, wantDefault)
	}
}
