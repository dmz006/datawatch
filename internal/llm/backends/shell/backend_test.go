package shell

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// TestLaunch_TmuxStderrSurfacesInError pins the error-visibibility
// guarantee: Launch sends the command via the shared llm.SendKeys helper,
// so a tmux failure carries tmux's own stderr text instead of a bare
// "exit status 1". Driven with a fake tmux on PATH that prints a distinct
// stderr message and exits nonzero — no real tmux server is started. Both
// the interactive-shell branch (empty scriptPath) and the script branch
// are covered.
func TestLaunch_TmuxStderrSurfacesInError(t *testing.T) {
	dir := t.TempDir()
	const want = "FAKE-TMUX-STDERR-SHELL"
	fakeTmux := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\necho '" + want + "' >&2\nexit 3\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cases := []struct {
		name string
		b    *Backend
	}{
		{"InteractiveShell", &Backend{scriptPath: ""}},
		{"Script", &Backend{scriptPath: "/nonexistent/script-xyz.sh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Launch(context.Background(), "task", "cs-err", dir, "")
			if err == nil {
				t.Fatal("expected the failing fake tmux to produce an error")
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error must carry tmux stderr %q, got: %v", want, err)
			}
			// The "tmux send-keys" wrapper only exists in llm.SendKeys' error
			// path — this is what proves Launch went through the shared helper
			// rather than a bare exec.Command("tmux", ...).
			if !strings.Contains(err.Error(), "tmux send-keys") {
				t.Errorf("error must be wrapped by the shared llm.SendKeys helper (\"tmux send-keys\"), got: %v", err)
			}
		})
	}
}

// TestLaunch_SendKeysLengthGuardApplies pins the second shared-helper
// guarantee at this call site: commands longer than
// llm.MaxSendKeysCommand bytes are rejected up front with a clear error
// naming the limit, and nothing is exec'd at all — no fake tmux is placed
// on PATH in this test, so any stray exec would fail differently.
func TestLaunch_SendKeysLengthGuardApplies(t *testing.T) {
	// Oversize via the project dir, which every Launch branch inlines
	// (interactive `cd '...'` and the script form alike).
	hugeDir := strings.Repeat("p", llm.MaxSendKeysCommand+1000)
	// The script form with an oversize task moves the task to a file, so its
	// oversize project dir can only fail at the task-file write (the dir cannot
	// be created). That error is returned before any exec, which is what this
	// test pins for that form.
	cases := []struct {
		name    string
		b       *Backend
		wantErr string
	}{
		{"InteractiveShell", &Backend{scriptPath: ""}, "command too long"},
		{"Script", &Backend{scriptPath: "/nonexistent/script-xyz.sh"}, "write oversize task file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Launch(context.Background(), strings.Repeat("t", 100), "cs-guard", hugeDir, "")
			if err == nil {
				t.Fatal("expected the launch to reject the oversize command")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected a %q error, got: %v", tc.wantErr, err)
			}
			if tc.name == "InteractiveShell" && !strings.Contains(err.Error(), strconv.Itoa(llm.MaxSendKeysCommand)) {
				t.Errorf("guard error must name the %d-byte limit, got: %v", llm.MaxSendKeysCommand, err)
			}
			if strings.Contains(err.Error(), "tmux send-keys") {
				t.Errorf("rejection happens before any exec, so no exec error should be present, got: %v", err)
			}
		})
	}
}

// TestLaunch_ScriptFormShortTaskInline pins the script form for a short task:
// the task stays inline in the command (no task file is written, and no
// .datawatch folder appears in the project directory), and the recorded
// command matches the inline format exactly. A fake tmux that exits 0 stands
// in for the real one, so no tmux server is started.
func TestLaunch_ScriptFormShortTaskInline(t *testing.T) {
	dir := t.TempDir()
	fakeTmux := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fakeTmux, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	records := make(chan CommandRecord, 4)
	SetRecorder(func(rec CommandRecord) { records <- rec })
	t.Cleanup(func() { SetRecorder(nil) })

	const task = "short task"
	b := &Backend{scriptPath: "/nonexistent/script-xyz.sh"}
	if err := b.Launch(context.Background(), task, "cs-short", dir, ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	var rec CommandRecord
	select {
	case rec = <-records:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the command record")
	}

	want := "'/nonexistent/script-xyz.sh' 'short task' '" + dir + "'; echo 'DATAWATCH_COMPLETE: shell done'"
	if rec.Command != want {
		t.Errorf("command mismatch\n got: %q\nwant: %q", rec.Command, want)
	}
	if rec.TaskFile != "" {
		t.Errorf("short task must stay inline, got TaskFile %q", rec.TaskFile)
	}
	if _, err := os.Stat(filepath.Join(dir, ".datawatch")); !os.IsNotExist(err) {
		t.Errorf("no .datawatch folder may exist for an inline task, stat err: %v", err)
	}
}

// TestLaunch_ScriptFormLongTaskByFile pins the script form for an oversize
// task: once the inline command would exceed llm.MaxSendKeysCommand, the task
// moves to a 0o600 file under the project's .datawatch folder and the recorded
// command passes that file path as the first script argument instead of the
// task text. The fake tmux exits 0 without running the command, so the task
// file stays in place for the assertions.
func TestLaunch_ScriptFormLongTaskByFile(t *testing.T) {
	dir := t.TempDir()
	fakeTmux := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fakeTmux, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	records := make(chan CommandRecord, 4)
	SetRecorder(func(rec CommandRecord) { records <- rec })
	t.Cleanup(func() { SetRecorder(nil) })

	// Repeated text with newlines, trimmed to exactly 20000 bytes.
	const unit = "long task line of repeated text\n"
	task := strings.Repeat(unit, 20000/len(unit)+1)[:20000]

	b := &Backend{scriptPath: "/nonexistent/script-xyz.sh"}
	if err := b.Launch(context.Background(), task, "cs-long", dir, ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	var rec CommandRecord
	select {
	case rec = <-records:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the command record")
	}

	if rec.TaskFile == "" {
		t.Fatal("oversize task must be written to a task file, got empty TaskFile")
	}
	rel, err := filepath.Rel(filepath.Join(dir, ".datawatch"), rec.TaskFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Errorf("task file %q must lie under %s/.datawatch (rel %q, err %v)", rec.TaskFile, dir, rel, err)
	}

	got, err := os.ReadFile(rec.TaskFile)
	if err != nil {
		t.Fatalf("read task file: %v", err)
	}
	if string(got) != task {
		t.Errorf("task file content mismatch: got %d bytes, want %d bytes", len(got), len(task))
	}

	info, err := os.Stat(rec.TaskFile)
	if err != nil {
		t.Fatalf("stat task file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("task file permissions = %o, want 600", perm)
	}

	if len(rec.Command) > llm.MaxSendKeysCommand {
		t.Errorf("command length %d exceeds MaxSendKeysCommand %d", len(rec.Command), llm.MaxSendKeysCommand)
	}
	wantPrefix := "'/nonexistent/script-xyz.sh' '" + rec.TaskFile + "' '"
	if !strings.HasPrefix(rec.Command, wantPrefix) {
		t.Errorf("command must pass the task file path as the first script argument\n got: %q\nwant prefix: %q", rec.Command, wantPrefix)
	}
	if strings.Contains(rec.Command, "long task line") {
		t.Errorf("command must not contain the task text")
	}
}

// TestLaunch_InteractiveOversizeTaskRefused pins the interactive-shell branch
// for an oversize task: the task is echoed inline into the command, so a
// 20000-byte task pushes the command past llm.MaxSendKeysCommand. Launch must
// reject it with the shared guard's error before any exec, so the fake tmux on
// PATH (which drops a marker file in its own folder) is never invoked and no
// .datawatch folder is created in the project directory.
func TestLaunch_InteractiveOversizeTaskRefused(t *testing.T) {
	binDir := t.TempDir()
	projectDir := t.TempDir()
	marker := filepath.Join(binDir, "tmux-invoked")
	fakeTmux := filepath.Join(binDir, "tmux")
	script := "#!/bin/sh\ntouch \"$(dirname \"$0\")/tmux-invoked\"\nexit 0\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Repeated text with newlines, trimmed to exactly 20000 bytes.
	const unit = "oversize interactive task line of repeated text\n"
	task := strings.Repeat(unit, 20000/len(unit)+1)[:20000]

	b := &Backend{scriptPath: ""}
	err := b.Launch(context.Background(), task, "cs-oversize-interactive", projectDir, "")
	if err == nil {
		t.Fatal("expected Launch to refuse an oversize interactive task")
	}
	if !strings.Contains(err.Error(), "command too long") {
		t.Errorf("error must contain %q, got: %v", "command too long", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(llm.MaxSendKeysCommand)) {
		t.Errorf("error must name the %d-byte limit, got: %v", llm.MaxSendKeysCommand, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Errorf("fake tmux must never be invoked for a refused command, marker stat err: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, ".datawatch")); !os.IsNotExist(statErr) {
		t.Errorf("no .datawatch folder may be created in the project directory, stat err: %v", statErr)
	}
}
