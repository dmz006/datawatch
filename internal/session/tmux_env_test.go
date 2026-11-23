package session

import (
	"strings"
	"testing"
)

func TestTmuxCommandStripsTmuxEnv(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	t.Setenv("TMUX_TMPDIR", "/run/datawatch/sockets")

	cmd := TmuxCommand("new-session", "-d", "-s", "test")

	foundTmux := false
	foundTmpdir := false
	for _, kv := range cmd.Env {
		switch {
		case kv == "TMUX" || strings.HasPrefix(kv, "TMUX="):
			t.Errorf("TMUX variable should be stripped, but found %q in cmd env", kv)
			foundTmux = true
		case strings.HasPrefix(kv, "TMUX_TMPDIR="):
			foundTmpdir = true
		}
	}
	if foundTmux {
		t.Fatal("TMUX must be absent from the cmd env")
	}
	if !foundTmpdir {
		t.Fatal("TMUX_TMPDIR must be present in the cmd env")
	}
	if len(cmd.Args) == 0 || cmd.Args[0] != "tmux" {
		t.Fatalf("expected tmux command, got args %v", cmd.Args)
	}
}
