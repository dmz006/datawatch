//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureTmuxTmpdirEnv is a regression test for an incident found live
// 2026-10-09: `datawatch restart`'s old daemonize() path started a fresh
// daemon outside systemd's management, so it never inherited
// datawatch.env's TMUX_TMPDIR. Every tmux call in internal/session/tmux.go
// is a bare exec.Command("tmux", ...) that relies entirely on inherited
// env, so they all silently hit tmux's default (no-server) socket instead
// of the dedicated one every live session's pane actually lived on —
// ResumeMonitors then marked 3 genuinely-alive sessions StateFailed.
// ensureTmuxTmpdirEnv now runs at daemon startup (runStart's --foreground
// branch) before any session/tmux code executes.
func TestEnsureTmuxTmpdirEnv(t *testing.T) {
	t.Run("leaves an already-set TMUX_TMPDIR alone", func(t *testing.T) {
		t.Setenv("TMUX_TMPDIR", "/run/dw-tmux")
		if got := ensureTmuxTmpdirEnv(); got != "" {
			t.Fatalf("ensureTmuxTmpdirEnv() = %q, want \"\" (already set)", got)
		}
		if got := os.Getenv("TMUX_TMPDIR"); got != "/run/dw-tmux" {
			t.Fatalf("TMUX_TMPDIR = %q, want unchanged", got)
		}
	})

	t.Run("no dedicated socket present — stays unset", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("TMUX_TMPDIR", "")
		if got := ensureTmuxTmpdirEnv(); got != "" {
			t.Fatalf("ensureTmuxTmpdirEnv() = %q, want \"\" with no dedicated socket", got)
		}
		if got := os.Getenv("TMUX_TMPDIR"); got != "" {
			t.Fatalf("TMUX_TMPDIR = %q, want still unset", got)
		}
	})

	// Reproduces the exact incident: a daemon started with TMUX_TMPDIR
	// unset must still find the dedicated ~/.datawatch/tmux socket
	// datawatch-tmux.service (or a prior daemon generation) already
	// created, and set it in its own environment before any tmux call.
	t.Run("falls back to the dedicated ~/.datawatch/tmux socket", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("TMUX_TMPDIR", "")

		dir := filepath.Join(home, ".datawatch", "tmux")
		sockDir := filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()))
		if err := os.MkdirAll(sockDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sockDir, "default"), nil, 0o600); err != nil {
			t.Fatal(err)
		}

		got := ensureTmuxTmpdirEnv()
		if got != dir {
			t.Fatalf("ensureTmuxTmpdirEnv() = %q, want %q", got, dir)
		}
		if envVal := os.Getenv("TMUX_TMPDIR"); envVal != dir {
			t.Fatalf("TMUX_TMPDIR after ensureTmuxTmpdirEnv() = %q, want %q — subsequent tmux exec.Command calls would still miss the dedicated socket", envVal, dir)
		}
	})
}

// TestParseSystemdShowMainPID guards against the exact bug caught live
// while writing this fix: `systemctl --user show datawatch.service
// --property=MainPID,ActiveState --value` did NOT return the values in
// the requested property order — it printed ActiveState's value
// ("active") first and MainPID's ("1582728") second, the reverse of what
// the original positional parsing assumed. That version would have
// silently always returned (0, false), so the systemd-delegation fix in
// this same release would never actually have fired. Parsing switched to
// key=value lines (no --value flag), which are order-independent; this
// test fixture is the literal output captured from the real unit.
func TestParseSystemdShowMainPID(t *testing.T) {
	cases := []struct {
		name       string
		out        string
		wantPID    int
		wantActive bool
	}{
		{
			name:       "ActiveState before MainPID (the real order observed live)",
			out:        "ActiveState=active\nMainPID=1582728\n",
			wantPID:    1582728,
			wantActive: true,
		},
		{
			name:       "MainPID before ActiveState (must still work)",
			out:        "MainPID=1582728\nActiveState=active\n",
			wantPID:    1582728,
			wantActive: true,
		},
		{
			name:       "inactive unit reports MainPID=0",
			out:        "ActiveState=inactive\nMainPID=0\n",
			wantPID:    0,
			wantActive: false,
		},
		{
			name:       "unit doesn't exist — systemctl prints empty values",
			out:        "ActiveState=\nMainPID=\n",
			wantPID:    0,
			wantActive: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pid, active := parseSystemdShowMainPID([]byte(c.out))
			if pid != c.wantPID || active != c.wantActive {
				t.Errorf("parseSystemdShowMainPID(%q) = (%d, %v), want (%d, %v)", c.out, pid, active, c.wantPID, c.wantActive)
			}
		})
	}
}
