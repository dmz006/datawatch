package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatePathWorktreeDirUnderDataDir: the default PRD worktree root lives
// under the configured data directory (a decoy HOME must not leak into it),
// and the result for the default data directory matches the previous
// home/.datawatch/prd-worktrees location.
func TestStatePathWorktreeDirUnderDataDir(t *testing.T) {
	// Real home (before the decoy) defines the historical location for a
	// daemon that never customised its data directory on this machine.
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	// Decoy HOME: any fallback to the home directory is observable.
	decoyHome := t.TempDir()
	t.Setenv("HOME", decoyHome)
	if got, err := os.UserHomeDir(); err != nil || got != decoyHome {
		t.Fatalf("decoy HOME not honoured: UserHomeDir() = %q (%v), want %q", got, err, decoyHome)
	}

	// 1. A temp (configured) data directory: the worktree root sits under it.
	dataDir := t.TempDir()
	got := DefaultPRDWorktreeDirFor(dataDir)
	want := filepath.Join(dataDir, "prd-worktrees")
	if got != want {
		t.Fatalf("DefaultPRDWorktreeDirFor(%q) = %q, want %q", dataDir, got, want)
	}
	if !filepath.IsAbs(got) || !strings.HasPrefix(got, dataDir+string(filepath.Separator)) {
		t.Fatalf("worktree root %q is not under data dir %q", got, dataDir)
	}

	// 2. The default data directory (<home>/.datawatch) must yield exactly
	// the previous location — with the decoy HOME that is
	// <decoy>/.datawatch/prd-worktrees, and it must equal the historical
	// DefaultPRDWorktreeDir() result.
	defDataDir := filepath.Join(decoyHome, ".datawatch")
	gotDef := DefaultPRDWorktreeDirFor(defDataDir)
	if wantDef := filepath.Join(decoyHome, ".datawatch", "prd-worktrees"); gotDef != wantDef {
		t.Fatalf("DefaultPRDWorktreeDirFor(%q) = %q, want %q", defDataDir, gotDef, wantDef)
	}
	if prev := DefaultPRDWorktreeDir(); gotDef != prev {
		t.Fatalf("DefaultPRDWorktreeDirFor(default data dir) = %q, previous location = %q", gotDef, prev)
	}
	// On the real machine the same rule holds for the real home.
	if gotReal := DefaultPRDWorktreeDirFor(filepath.Join(realHome, ".datawatch")); gotReal != filepath.Join(realHome, ".datawatch", "prd-worktrees") {
		t.Fatalf("DefaultPRDWorktreeDirFor(%q) = %q", realHome, gotReal)
	}

	// 3. Empty data directory falls back to the previous home-based default
	// (the decoy home, so a regression to the home path is still visible).
	if gotEmpty := DefaultPRDWorktreeDirFor(""); gotEmpty != filepath.Join(decoyHome, ".datawatch", "prd-worktrees") {
		t.Fatalf("DefaultPRDWorktreeDirFor(\"\") = %q, want %q", gotEmpty, filepath.Join(decoyHome, ".datawatch", "prd-worktrees"))
	}
}
