package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSystemctl puts a systemctl stand-in first on PATH that prints state and
// exits with rc, the way `systemctl --user is-active` does.
func fakeSystemctl(t *testing.T, state string, rc int) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho '" + state + "'\nexit " + itoa(rc) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

func TestShellProbeDaemonActiveReadsNonZeroExitStates(t *testing.T) {
	cases := []struct {
		state  string
		rc     int
		active bool
		errOK  bool
	}{
		{"active", 0, true, false},
		{"inactive", 3, false, false}, // systemctl exits 3 for an inactive unit
		{"failed", 3, false, false},
		{"activating", 3, false, false},
		{"unknown garbage", 1, false, true}, // no recognised state: unknown
	}
	for _, c := range cases {
		fakeSystemctl(t, c.state, c.rc)
		p := NewShellSupervisorWatchHostProbe(nil, "node-a")
		active, err := p.DaemonActive("node-a")
		if (err != nil) != c.errOK {
			t.Errorf("state %q rc %d: err = %v, want error %v", c.state, c.rc, err, c.errOK)
		}
		if active != c.active {
			t.Errorf("state %q rc %d: active = %v, want %v", c.state, c.rc, active, c.active)
		}
	}
}

func TestShellProbeDaemonActiveOtherNodeIsUnknown(t *testing.T) {
	fakeSystemctl(t, "inactive", 3)
	p := NewShellSupervisorWatchHostProbe(nil, "node-a")
	if _, err := p.DaemonActive("node-b"); err == nil {
		t.Fatal("a node other than the local one must read as unknown")
	}
}
