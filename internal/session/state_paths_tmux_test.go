package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestStatePathTmuxSocketDirUnderDataDir pins the daemon's tmux socket
// fallback to the configured data directory: with a decoy socket under a
// different HOME and no TMUX_TMPDIR, TmuxSocketDirFor must still return the
// <dataDir>/tmux copy, not anything under HOME.
func TestStatePathTmuxSocketDirUnderDataDir(t *testing.T) {
	uid := fmt.Sprintf("tmux-%d", os.Getuid())

	dataDir := t.TempDir()
	sockDir := filepath.Join(dataDir, "tmux", uid)
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sockDir, "default"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	decoyHome := t.TempDir()
	homeSock := filepath.Join(decoyHome, ".datawatch", "tmux", uid)
	if err := os.MkdirAll(homeSock, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeSock, "default"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", decoyHome)
	t.Setenv("TMUX_TMPDIR", "")

	if got, want := TmuxSocketDirFor(dataDir), filepath.Join(dataDir, "tmux"); got != want {
		t.Fatalf("TmuxSocketDirFor(%q) = %q, want %q", dataDir, got, want)
	}
}
