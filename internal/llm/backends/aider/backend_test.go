package aider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/testhome"
)

// installFakeTmux puts a PATH-resolvable fake `tmux` in dir that records
// its argv (one argument per line) to argvFile. No real tmux server is
// ever touched.
//
// Recording uses `printf '%s\n'` inside a POSIX `for` loop rather than the
// NUL-separated `printf '%s\0'` form: NUL bytes in printf formats are not
// guaranteed on every shell/busybox build on every host, while `%s\n` is
// POSIX. The command Launch sends is a single line (the task body lives in
// a scratch file, never inline), so line separation loses nothing.
func installFakeTmux(t *testing.T, dir, argvFile string) {
	t.Helper()
	fakeTmux := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + shellEscape(argvFile) + "'\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

func recordedSendKeysCmd(t *testing.T, argvFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read recorded tmux argv: %v", err)
	}
	// One argument per line: tmux send-keys -t <session> <cmd> Enter
	args := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(args) < 2 || args[len(args)-2] == "" {
		t.Fatalf("fake tmux recorded %d argv element(s) (%q); want `... <cmd> Enter` with a non-empty command", len(args), string(raw))
	}
	return args[len(args)-2]
}

// TestLaunch_LongTaskGoesThroughScratchFile proves that Launching with a
// 40 KB task (multi-KB spec inline would blow past tmux's ~16 KB send-keys
// limit) emits a short command containing the `$(cat '...')` expression,
// and that the scratch file holds the exact task text.
func TestLaunch_LongTaskGoesThroughScratchFile(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	installFakeTmux(t, dir, argvFile)
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir

	task := "SPEC\n" + strings.Repeat("x", 40*1024)
	b := New("aider")
	if err := b.Launch(context.Background(), task, "cs-test", "/proj", "/tmp/out.log"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedSendKeysCmd(t, argvFile)

	if len(cmd) >= len(task) {
		t.Errorf("Launch command length %d must be far shorter than the %d-byte task", len(cmd), len(task))
	}
	i := strings.Index(cmd, "$(cat '")
	if i < 0 {
		t.Fatalf("expected task delivered via `$(cat '...')`, got: %.200s", cmd)
	}
	if strings.Contains(cmd, "xxxxxxxxxxxxxxxx") {
		t.Errorf("task text must not appear inline in the command, got: %.200s", cmd)
	}
	// Extract the scratch file path and verify it holds the full task.
	rest := cmd[i+len("$(cat '"):]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatalf("unterminated scratch file path in: %s", rest)
	}
	path := rest[:end]
	if !filepath.IsAbs(path) {
		t.Fatalf("scratch file path = %q, want absolute", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read scratch file: %v", err)
	}
	if string(got) != task {
		t.Errorf("scratch file contents = %d bytes, want the full %d-byte task text", len(got), len(task))
	}
	// Cleanup must be scheduled before the completion echo.
	if !strings.Contains(cmd, "rm -f '"+path+"'") {
		t.Errorf("expected `rm -f` cleanup of the scratch file in: %.400s", cmd)
	}
	rmIdx := strings.Index(cmd, "rm -f '"+path+"'")
	echoIdx := strings.Index(cmd, "DATAWATCH_COMPLETE")
	if rmIdx >= echoIdx {
		t.Errorf("rm -f cleanup must come before the DATAWATCH_COMPLETE echo, got: %.400s", cmd)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: aider done'") {
		t.Errorf("completion marker must be echoed unconditionally, got: %.400s", cmd)
	}
}

func TestLaunch_TaskWithSingleQuotesStillDeliveredIntact(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	installFakeTmux(t, dir, argvFile)
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir

	task := "don't break it's a test\nwith a newline"
	b := New("aider")
	if err := b.Launch(context.Background(), task, "cs-test", "/proj", ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedSendKeysCmd(t, argvFile)
	if !strings.Contains(cmd, "$(cat '") {
		t.Fatalf("expected task delivered via a scratch file, got: %s", cmd)
	}
	if strings.Contains(cmd, "break") {
		t.Errorf("task text must not appear inline in the command, got: %s", cmd)
	}
}

// TestLaunch_FallsBackToInlineWhenScratchFileFails proves that if writing
// the scratch file fails (unwritable temp dir), Launch falls back to the
// old inline form so the task is still delivered.
func TestLaunch_FallsBackToInlineWhenScratchFileFails(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	installFakeTmux(t, dir, argvFile)
	testhome.PrivateTmp(t) // any scratch file lands in the test's temp dir
	// Point TMPDIR at a path that doesn't exist so os.CreateTemp fails.
	t.Setenv("TMPDIR", filepath.Join(dir, "definitely-missing"))

	task := "small inline task"
	b := New("aider")
	if err := b.Launch(context.Background(), task, "cs-test", "/proj", ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedSendKeysCmd(t, argvFile)
	if strings.Contains(cmd, "$(cat '") {
		t.Errorf("scratch file write failed; command must not reference a cat file, got: %s", cmd)
	}
	if !strings.Contains(cmd, "--message 'small inline task'") {
		t.Errorf("expected inline fallback form in: %s", cmd)
	}
	if !strings.HasSuffix(cmd, "echo 'DATAWATCH_COMPLETE: aider done'") {
		t.Errorf("completion marker must still be echoed, got: %s", cmd)
	}
}

func TestLaunch_ProjectDirQuoting(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	installFakeTmux(t, dir, argvFile)
	testhome.PrivateTmp(t) // scratch task file is removed with the test's temp dir

	b := New("aider")
	if err := b.Launch(context.Background(), "task", "cs-test", "/proj's/dir", ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cmd := recordedSendKeysCmd(t, argvFile)
	if !strings.Contains(cmd, `cd '/proj'\''s/dir'`) {
		t.Errorf("expected single quote in project dir to be shell-escaped, got: %.400s", cmd)
	}
}

func TestBackend_Contract(t *testing.T) {
	b := New("")
	if b.Name() != "aider" {
		t.Errorf("Name() = %q, want \"aider\"", b.Name())
	}
	if b.SupportsInteractiveInput() {
		t.Error("SupportsInteractiveInput() must be false — aider --message is one-shot")
	}
	if tc, ok := b.(interface{ HasToolAccess() bool }); !ok || !tc.HasToolAccess() {
		t.Error("HasToolAccess() must be true — aider is a real tool-capable coding agent")
	}
}

func TestBackend_DefaultsBinaryToAider(t *testing.T) {
	if got := New("").(*Backend); got.binary != "aider" {
		t.Errorf("default binary = %q, want \"aider\"", got.binary)
	}
}

// Compile-time contract check: aider.Backend must satisfy llm.Backend.
var _ llm.Backend = New("")
