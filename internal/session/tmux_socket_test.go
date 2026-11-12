package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTmuxSocketDir_EnvWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX_TMPDIR", "/run/dw-tmux")
	dataDir := t.TempDir()
	if got := TmuxSocketDirFor(dataDir); got != "/run/dw-tmux" {
		t.Fatalf("TmuxSocketDirFor(%q) = %q, want env value", dataDir, got)
	}
	if got, want := TmuxAttachCommand("cs-h-ab12", dataDir), "TMUX_TMPDIR=/run/dw-tmux tmux attach -t cs-h-ab12"; got != want {
		t.Fatalf("TmuxAttachCommand() = %q, want %q", got, want)
	}
}

func TestTmuxSocketDir_DefaultSocket(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX_TMPDIR", "")
	dataDir := t.TempDir()
	if got := TmuxSocketDirFor(dataDir); got != "" {
		t.Fatalf("TmuxSocketDirFor(%q) = %q, want empty with no dedicated socket", dataDir, got)
	}
	if got, want := TmuxAttachCommand("cs-h-ab12", dataDir), "tmux attach -t cs-h-ab12"; got != want {
		t.Fatalf("TmuxAttachCommand() = %q, want %q", got, want)
	}
	if got := (&TmuxManager{}).AttachCommand("cs-h-ab12", dataDir); got != "tmux attach -t cs-h-ab12" {
		t.Fatalf("AttachCommand() = %q", got)
	}
}

// An operator shell has no TMUX_TMPDIR, but the CLI must still point at the
// dedicated socket when datawatch-tmux.service has created it — and the
// fallback must be anchored at the daemon's configured data directory, not
// at whatever $HOME resolves to.
func TestTmuxSocketDir_FallsBackToDataDirSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMUX_TMPDIR", "")
	// Decoy socket under HOME: it must never satisfy the fallback.
	homeSock := filepath.Join(home, ".datawatch", "tmux", fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.MkdirAll(homeSock, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeSock, "default"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "tmux")
	sockDir := filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Socket dir without the socket itself is not enough.
	if got := TmuxSocketDirFor(dataDir); got != "" {
		t.Fatalf("TmuxSocketDirFor(%q) = %q, want empty before socket exists", dataDir, got)
	}
	if err := os.WriteFile(filepath.Join(sockDir, "default"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := TmuxSocketDirFor(dataDir); got != dir {
		t.Fatalf("TmuxSocketDirFor(%q) = %q, want %q", dataDir, got, dir)
	}
	if got, want := TmuxAttachCommand("x", dataDir), "TMUX_TMPDIR="+dir+" tmux attach -t x"; got != want {
		t.Fatalf("TmuxAttachCommand() = %q, want %q", got, want)
	}
}
