package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSessionGitDefault_DefaultFalse verifies session.git_default is false
// with no config, and stays false when the key is absent from a loaded config.
func TestSessionGitDefault_DefaultFalse(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Session.GitDefault {
		t.Error("DefaultConfig: session.git_default should default to false")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("session:\n  max_sessions: 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Session.GitDefault {
		t.Error("Load: session.git_default should stay false when unset")
	}
}

// TestSessionGitDefault_Override verifies `session.git_default: true` in a
// config file overrides the default.
func TestSessionGitDefault_Override(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("session:\n  git_default: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Session.GitDefault {
		t.Error("Load: session.git_default should honor an explicit true override")
	}
}
