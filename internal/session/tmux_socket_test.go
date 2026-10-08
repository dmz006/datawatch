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
	if got := TmuxSocketDir(); got != "/run/dw-tmux" {
		t.Fatalf("TmuxSocketDir() = %q, want env value", got)
	}
	if got, want := TmuxAttachCommand("cs-h-ab12"), "TMUX_TMPDIR=/run/dw-tmux tmux attach -t cs-h-ab12"; got != want {
		t.Fatalf("TmuxAttachCommand() = %q, want %q", got, want)
	}
}

func TestTmuxSocketDir_DefaultSocket(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX_TMPDIR", "")
	if got := TmuxSocketDir(); got != "" {
		t.Fatalf("TmuxSocketDir() = %q, want empty with no dedicated socket", got)
	}
	if got, want := TmuxAttachCommand("cs-h-ab12"), "tmux attach -t cs-h-ab12"; got != want {
		t.Fatalf("TmuxAttachCommand() = %q, want %q", got, want)
	}
	if got := (&TmuxManager{}).AttachCommand("cs-h-ab12"); got != "tmux attach -t cs-h-ab12" {
		t.Fatalf("AttachCommand() = %q", got)
	}
}

// An operator shell has no TMUX_TMPDIR, but the CLI must still point at the
// dedicated socket when datawatch-tmux.service has created it.
func TestTmuxSocketDir_FallsBackToDataDirSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMUX_TMPDIR", "")
	dir := filepath.Join(home, ".datawatch", "tmux")
	sockDir := filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Socket dir without the socket itself is not enough.
	if got := TmuxSocketDir(); got != "" {
		t.Fatalf("TmuxSocketDir() = %q, want empty before socket exists", got)
	}
	if err := os.WriteFile(filepath.Join(sockDir, "default"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := TmuxSocketDir(); got != dir {
		t.Fatalf("TmuxSocketDir() = %q, want %q", got, dir)
	}
	if got, want := TmuxAttachCommand("x"), "TMUX_TMPDIR="+dir+" tmux attach -t x"; got != want {
		t.Fatalf("TmuxAttachCommand() = %q, want %q", got, want)
	}
}
