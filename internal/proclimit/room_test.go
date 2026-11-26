package proclimit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ceiling struct {
	Starter
	bytes int64
}

func (c ceiling) CeilingBytes() int64 { return c.bytes }

func TestRoomAsksForTheLimitAndTheMargin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal:       100 kB\nMemAvailable:   26214400 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if free, err := freeMemory(path); err != nil || free != 25<<30 {
		t.Errorf("free = %d, %v", free, err)
	}
	for _, bad := range []string{"MemTotal: 100 kB\n", "MemAvailable: lots kB\n"} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := freeMemory(path); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	if err := Room(ceiling{bytes: 1 << 50}); err == nil || !strings.Contains(err.Error(), "free memory") ||
		!strings.Contains(err.Error(), "margin of 8 GiB") {
		t.Errorf("a limit no system has room for: %v", err)
	}
	// A Starter with no memory limit asks for no room.
	if err := Room(None(os.ErrPermission)); err != nil {
		t.Errorf("none: %v", err)
	}
}

func TestInsideTakesAwayTheWaysOut(t *testing.T) {
	in := []string{"PATH=/bin", "XDG_RUNTIME_DIR=/run/user/1", "DBUS_SESSION_BUS_ADDRESS=unix:path=/x", "TMUX=/s,1,0",
		"DISPLAY=:0", "SSH_AUTH_SOCK=/a", "DOCKER_HOST=x", "NOTIFY_SOCKET=/n", "TMUX_TMPDIR=/private", "HOME=/h", "XDG_RUNTIME_DIRECTORY=kept"}
	got := strings.Join(Inside(in), " ")
	if want := "PATH=/bin TMUX_TMPDIR=/private HOME=/h XDG_RUNTIME_DIRECTORY=kept"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
