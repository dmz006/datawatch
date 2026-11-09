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
