package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Hostname == "" {
		t.Error("Hostname should not be empty")
	}
	if cfg.DataDir == "" {
		t.Error("DataDir should not be empty")
	}
	if cfg.Signal.ConfigDir == "" {
		t.Error("Signal.ConfigDir should not be empty")
	}
	if cfg.Signal.DeviceName == "" {
		t.Error("Signal.DeviceName should default to hostname")
	}
	if cfg.Session.MaxSessions != 10 {
		t.Errorf("MaxSessions = %d, want 10", cfg.Session.MaxSessions)
	}
	if cfg.Session.InputIdleTimeout != 10 {
		t.Errorf("InputIdleTimeout = %d, want 10", cfg.Session.InputIdleTimeout)
	}
	if cfg.Session.TailLines != 20 {
		t.Errorf("TailLines = %d, want 20", cfg.Session.TailLines)
	}
	if cfg.Session.AlertContextLines != 10 {
		t.Errorf("AlertContextLines = %d, want 10", cfg.Session.AlertContextLines)
	}
	if cfg.Session.LLMBackend != "claude-code" {
		t.Errorf("LLMBackend = %q, want claude-code", cfg.Session.LLMBackend)
	}
	if cfg.Session.ClaudeBin != "claude" {
		t.Errorf("ClaudeBin = %q, want claude", cfg.Session.ClaudeBin)
	}
	if cfg.Session.DefaultProjectDir == "" {
		t.Error("DefaultProjectDir should not be empty")
	}
	if !cfg.Session.AutoGitCommit {
		t.Error("AutoGitCommit should default to true")
	}
	if !cfg.Server.Enabled {
		t.Error("Server.Enabled should default to true")
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
}

func TestLoad_NonExistent(t *testing.T) {
	cfg, err := Load("/nonexistent/path/to/config.yaml")
	if err != nil {
		t.Fatalf("Load non-existent file should return defaults, got error: %v", err)
	}
	if cfg.Session.MaxSessions != 10 {
		t.Errorf("MaxSessions = %d, want 10 (default)", cfg.Session.MaxSessions)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: [unclosed"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestLoad_Partial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
signal:
  account_number: "+12125551234"
  group_id: "abc123"
session:
  max_sessions: 5
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Signal.AccountNumber != "+12125551234" {
		t.Errorf("AccountNumber = %q, want +12125551234", cfg.Signal.AccountNumber)
	}
	if cfg.Signal.GroupID != "abc123" {
		t.Errorf("GroupID = %q, want abc123", cfg.Signal.GroupID)
	}
	if cfg.Session.MaxSessions != 5 {
		t.Errorf("MaxSessions = %d, want 5", cfg.Session.MaxSessions)
	}
	// Unset fields should keep defaults
	if cfg.Session.LLMBackend != "claude-code" {
		t.Errorf("LLMBackend = %q, want claude-code (default)", cfg.Session.LLMBackend)
	}
	if cfg.Session.TailLines != 20 {
		t.Errorf("TailLines = %d, want 20 (default)", cfg.Session.TailLines)
	}
}

func TestLoad_ZeroFieldsGetDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Explicit zeros should be replaced with defaults
	yaml := `
session:
  max_sessions: 0
  tail_lines: 0
  input_idle_timeout: 0
hostname: ""
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Session.MaxSessions != 10 {
		t.Errorf("MaxSessions = %d, want 10 (zero → default)", cfg.Session.MaxSessions)
	}
	if cfg.Session.TailLines != 20 {
		t.Errorf("TailLines = %d, want 20 (zero → default)", cfg.Session.TailLines)
	}
	if cfg.Session.InputIdleTimeout != 10 {
		t.Errorf("InputIdleTimeout = %d, want 10 (zero → default)", cfg.Session.InputIdleTimeout)
	}
	if cfg.Hostname == "" {
		t.Error("Hostname should not be empty after defaulting")
	}
}

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Signal.AccountNumber = "+12125551234"
	cfg.Signal.GroupID = "testgroup"
	cfg.Session.MaxSessions = 3
	cfg.Server.Port = 9090

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}

	if loaded.Signal.AccountNumber != cfg.Signal.AccountNumber {
		t.Errorf("AccountNumber: got %q, want %q", loaded.Signal.AccountNumber, cfg.Signal.AccountNumber)
	}
	if loaded.Signal.GroupID != cfg.Signal.GroupID {
		t.Errorf("GroupID: got %q, want %q", loaded.Signal.GroupID, cfg.Signal.GroupID)
	}
	if loaded.Session.MaxSessions != cfg.Session.MaxSessions {
		t.Errorf("MaxSessions: got %d, want %d", loaded.Session.MaxSessions, cfg.Session.MaxSessions)
	}
	if loaded.Server.Port != cfg.Server.Port {
		t.Errorf("Server.Port: got %d, want %d", loaded.Server.Port, cfg.Server.Port)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %04o, want 0600", perm)
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "config.yaml")

	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("Save with nested path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestConfigPath(t *testing.T) {
	p := ConfigPath()
	if p == "" {
		t.Error("ConfigPath should not be empty")
	}
	if filepath.Base(p) != "config.yaml" {
		t.Errorf("ConfigPath base = %q, want config.yaml", filepath.Base(p))
	}
}
