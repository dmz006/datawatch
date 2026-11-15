// Package config — project description round-trip through Save/Load.
//
// A saved project's description must survive a config save and a later load
// (the daemon restart reads the same file). This is the store-level proof
// that the description is a persistent part of the project record.

package config

import (
	"path/filepath"
	"testing"
)

func TestProjectDescription_SaveLoadRoundTrip(t *testing.T) {
	cfg := &Config{Git: GitConfig{ReleaseCIWindowMinutes: 60}}
	cfg.Projects = map[string]ProjectConfigEntry{
		"acme": {
			Dir:            "/srv/acme",
			DefaultBackend: "claude-code",
			Description:    "the billing service for the Automata",
		},
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := loaded.Projects["acme"]
	if !ok {
		t.Fatalf("project acme missing after load, projects=%+v", loaded.Projects)
	}
	if entry.Description != "the billing service for the Automata" {
		t.Errorf("description after load = %q, want %q", entry.Description, "the billing service for the Automata")
	}
	if entry.Dir != "/srv/acme" {
		t.Errorf("dir after load = %q, want %q", entry.Dir, "/srv/acme")
	}
	if entry.DefaultBackend != "claude-code" {
		t.Errorf("default_backend after load = %q, want %q", entry.DefaultBackend, "claude-code")
	}
}

// A second save must keep the description too: an update that changes only
// one other field (here the dir) and then saves again must not drop it.
func TestProjectDescription_UpdateKeepsDescription(t *testing.T) {
	cfg := &Config{Git: GitConfig{ReleaseCIWindowMinutes: 60}}
	cfg.Projects = map[string]ProjectConfigEntry{
		"acme": {Dir: "/srv/acme", Description: "first note", DefaultBackend: "claude-code"},
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg2, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry := cfg2.Projects["acme"]
	entry.Dir = "/srv/acme-v2"
	cfg2.Projects["acme"] = entry
	if err := Save(cfg2, path); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	cfg3, err := Load(path)
	if err != nil {
		t.Fatalf("Load 3: %v", err)
	}
	if got := cfg3.Projects["acme"]; got.Description != "first note" {
		t.Errorf("description after second save = %q, want %q", got.Description, "first note")
	}
	if got := cfg3.Projects["acme"]; got.Dir != "/srv/acme-v2" {
		t.Errorf("dir after update = %q, want %q", got.Dir, "/srv/acme-v2")
	}
}
