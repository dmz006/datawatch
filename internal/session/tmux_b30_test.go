// B30 — SendKeysWithSettle splits text + Enter into two tmux calls
// with a configurable settle delay between them, fixing the 2nd-Enter
// scheduled-command bug.

package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeTmuxOnPath writes a shell script to tmpDir/tmux that logs its
// argv (one call per line, tab-separated) to tmpDir/tmux.log and exits
// 0. Returns the PATH value to set. Skips on non-unix platforms where
// shebang scripts don't work the same.
func fakeTmuxOnPath(t *testing.T) (pathVal, logFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake tmux wrapper uses /bin/sh")
	}
	dir := t.TempDir()
	logFile = filepath.Join(dir, "tmux.log")
	script := "#!/bin/sh\n" +
		`printf "%s\n" "$*" >> ` + logFile + "\n" +
		"exit 0\n"
	p := filepath.Join(dir, "tmux")
	if err := os.WriteFile(p, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	// Prepend to PATH so exec.Command("tmux", ...) finds the fake.
	pathVal = dir + string(os.PathListSeparator) + os.Getenv("PATH")
	return
}

func TestSendKeysWithSettle_SettleZeroFallsBackToOneShot(t *testing.T) {
	pathVal, logFile := fakeTmuxOnPath(t)
	t.Setenv("PATH", pathVal)

	tm := &TmuxManager{}
	if err := tm.SendKeysWithSettle("s", "hello", 0); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(logFile)
	got := strings.TrimSpace(string(b))
	lines := strings.Split(got, "\n")
	if len(lines) != 1 {
		t.Fatalf("settle=0 should produce 1 tmux call, got %d:\n%s", len(lines), got)
	}
	// Single-call form: send-keys -t s hello Enter
	if !strings.Contains(lines[0], "send-keys") ||
		!strings.Contains(lines[0], "hello") ||
		!strings.Contains(lines[0], "Enter") {
		t.Errorf("unexpected one-shot argv: %q", lines[0])
	}
}

func TestSendKeysWithSettle_NonZeroEmitsTwoCalls(t *testing.T) {
	pathVal, logFile := fakeTmuxOnPath(t)
	t.Setenv("PATH", pathVal)

	tm := &TmuxManager{}
	start := time.Now()
	if err := tm.SendKeysWithSettle("s", "hello", 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("expected ~50ms settle, got %v", elapsed)
	}
	b, _ := os.ReadFile(logFile)
	got := strings.TrimSpace(string(b))
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("settle>0 should produce 2 tmux calls, got %d:\n%s", len(lines), got)
	}
	// First call: literal push with -l flag (no Enter)
	if !strings.Contains(lines[0], "-l") || !strings.Contains(lines[0], "hello") {
		t.Errorf("first call should be -l literal push: %q", lines[0])
	}
	if strings.Contains(lines[0], "Enter") {
		t.Errorf("first call should not contain Enter: %q", lines[0])
	}
	// Second call: send Enter only
	if !strings.Contains(lines[1], "Enter") {
		t.Errorf("second call should send Enter: %q", lines[1])
	}
}

func TestManager_SetScheduleSettleMs(t *testing.T) {
	m := &Manager{}
	m.SetScheduleSettleMs(200)
	if m.ScheduleSettleMs() != 200 {
		t.Errorf("got %d want 200", m.ScheduleSettleMs())
	}
	// Negative clamps to 0.
	m.SetScheduleSettleMs(-50)
	if m.ScheduleSettleMs() != 0 {
		t.Errorf("negative should clamp to 0, got %d", m.ScheduleSettleMs())
	}
}
