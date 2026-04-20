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

// v4.0.4 (B34): SendKeys used to go one-shot when settle==0, which
// caused modern TUIs to eat the Enter. All tmux sends are now
// two-step with a small default settle. This test verifies that:
// settle==0 clamps to the default and produces 2 tmux calls.
func TestSendKeysWithSettle_SettleZeroClampsToDefault(t *testing.T) {
	pathVal, logFile := fakeTmuxOnPath(t)
	t.Setenv("PATH", pathVal)

	tm := &TmuxManager{}
	if err := tm.SendKeysWithSettle("s", "hello", 0); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("settle=0 should clamp to default and produce 2 tmux calls, got %d:\n%s", len(lines), string(b))
	}
	if !strings.Contains(lines[0], "-l") || !strings.Contains(lines[0], "hello") {
		t.Errorf("first call should be -l literal push: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Enter") {
		t.Errorf("second call should send Enter: %q", lines[1])
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

// v4.0.2 — regression test for the "sends command + blank line,
// operator has to press Enter again" bug. When the caller passes
// keys with a trailing newline (e.g. from a copy-pasted command or
// a schedule store entry that was line-delimited), both SendKeys
// and SendKeysWithSettle must strip that newline so the explicit
// Enter keypress actually triggers submission instead of just
// adding another blank line in a TUI compose buffer.
func TestTrimTrailingNewlines(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"hello":         "hello",
		"hello\n":       "hello",
		"hello\r\n":     "hello",
		"hello\n\n\n":   "hello",
		"hello world\n": "hello world",
		"\n":            "",
		"\r\n":          "",
	}
	for in, want := range cases {
		if got := trimTrailingNewlines(in); got != want {
			t.Errorf("trimTrailingNewlines(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSendKeys_StripsTrailingNewlineBeforeEnter(t *testing.T) {
	pathVal, logFile := fakeTmuxOnPath(t)
	t.Setenv("PATH", pathVal)

	tm := &TmuxManager{}
	if err := tm.SendKeys("s", "hello\n"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(logFile)
	// v4.0.4 (B34): SendKeys is always two-step now to avoid the
	// bracketed-paste + TUI race that made claude-code / ink eat
	// the Enter. First tmux call is `-l hello` (literal, no newline);
	// second is `Enter`.
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("SendKeys should make 2 tmux calls, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "-l") || !strings.Contains(lines[0], "hello") {
		t.Errorf("first call should be -l literal push: %q", lines[0])
	}
	if strings.Contains(lines[0], `\n`) {
		t.Errorf("literal push leaked \\n: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Enter") {
		t.Errorf("second call should send Enter: %q", lines[1])
	}
}

func TestSendKeysWithSettle_StripsTrailingNewline(t *testing.T) {
	pathVal, logFile := fakeTmuxOnPath(t)
	t.Setenv("PATH", pathVal)

	tm := &TmuxManager{}
	if err := tm.SendKeysWithSettle("s", "hello\n\n", 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 tmux calls, got %d: %v", len(lines), lines)
	}
	if strings.Contains(lines[0], `\n`) {
		t.Errorf("literal push leaked \\n: %q", lines[0])
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
