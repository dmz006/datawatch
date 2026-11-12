package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
)

// TestLaunch_OversizeScriptTaskPassedByFile pins the file-path branch of the
// script form: when the inline command would exceed llm.MaxSendKeysCommand,
// Launch writes the task to a file under the project's .datawatch folder,
// sends a command that passes that path to the script, removes the file
// after the script exits, and records the path as TaskFile. A fake tmux on
// PATH captures the command so no real tmux server is started.
func TestLaunch_OversizeScriptTaskPassedByFile(t *testing.T) {
	bin := t.TempDir()
	projectDir := t.TempDir()
	argsFile := filepath.Join(bin, "args")
	fakeTmux := filepath.Join(bin, "tmux")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + argsFile + "'\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ch := make(chan CommandRecord, 1)
	SetRecorder(func(r CommandRecord) { ch <- r })
	defer SetRecorder(nil)

	task := strings.Repeat("x", llm.MaxSendKeysCommand+500)
	b := &Backend{scriptPath: "/opt/run-task.sh"}
	if err := b.Launch(context.Background(), task, "cs-file", projectDir, ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read captured tmux args: %v", err)
	}
	var cmd string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "DATAWATCH_COMPLETE") {
			cmd = line
		}
	}
	if cmd == "" {
		t.Fatalf("captured tmux args carry no launch command: %q", raw)
	}
	if len(cmd) > llm.MaxSendKeysCommand {
		t.Fatalf("launch command is %d bytes, still over the limit", len(cmd))
	}
	if strings.Contains(cmd, task) {
		t.Fatal("launch command must not carry the oversize task text inline")
	}

	rec := <-ch
	if rec.TaskFile == "" {
		t.Fatal("CommandRecord.TaskFile must name the task file")
	}
	if !strings.HasPrefix(rec.TaskFile, projectDir+string(os.PathSeparator)+".datawatch") {
		t.Errorf("task file %q must live under the project's .datawatch folder", rec.TaskFile)
	}
	if !strings.Contains(cmd, "'"+rec.TaskFile+"'") || !strings.Contains(cmd, "rm -f '"+rec.TaskFile+"'") {
		t.Errorf("command must pass and then remove the task file %q: %s", rec.TaskFile, cmd)
	}
	got, err := os.ReadFile(rec.TaskFile)
	if err != nil {
		t.Fatalf("task file must exist until the script runs: %v", err)
	}
	if string(got) != task {
		t.Errorf("task file content mismatch: got %d bytes, want %d", len(got), len(task))
	}
}
