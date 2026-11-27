// A script's own exit status decides which marker the typed command prints,
// and the typed command itself never reads as either one.

package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// typedCommand launches script through the backend with a fake tmux and
// returns the command it would type.
func typedCommand(t *testing.T, script, task, projectDir string) CommandRecord {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	path := os.Getenv("PATH")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+path)
	records := make(chan CommandRecord, 1)
	SetRecorder(func(rec CommandRecord) { records <- rec })
	defer SetRecorder(nil)
	if err := (&Backend{scriptPath: script}).Launch(context.Background(), task, "cs-m", projectDir, ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Setenv("PATH", path)
	select {
	case rec := <-records:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatal("no command record")
		return CommandRecord{}
	}
}

func TestScriptExitStatusPicksOneMarker(t *testing.T) {
	long := strings.Repeat("long task text\n", 1500)
	for _, sh := range []string{"sh", "bash", "dash", "zsh", "fish"} {
		bin, err := exec.LookPath(sh)
		if err != nil {
			continue
		}
		for label, task := range map[string]string{"inline task": "short task", "task by file": long} {
			for _, exit := range []string{"0", "3"} {
				t.Run(sh+"/"+label+"/exit "+exit, func(t *testing.T) {
					dir := t.TempDir()
					script := filepath.Join(dir, "job.sh")
					if err := os.WriteFile(script, []byte("#!/bin/sh\necho working\nexit "+exit+"\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
						t.Fatal(err)
					}
					rec := typedCommand(t, script, task, dir)
					if (rec.TaskFile != "") != (label == "task by file") {
						t.Fatalf("task file = %q", rec.TaskFile)
					}
					out, _ := exec.Command(bin, "-c", rec.Command).CombinedOutput()
					want := "working\nDATAWATCH_COMPLETE: shell done\n"
					if exit != "0" {
						want = "working\nDATAWATCH_FAILED: shell script failed\n"
					}
					if string(out) != want {
						t.Errorf("output = %q, want %q", out, want)
					}
					if rec.TaskFile != "" {
						if _, err := os.Stat(rec.TaskFile); !os.IsNotExist(err) {
							t.Errorf("task file left behind: %v", err)
						}
					}
				})
			}
		}
	}
}

// A terminal wraps a long command, and any wrapped fragment can start where a
// marker starts. So the typed text holds neither marker.
func TestTypedCommandHoldsNoMarker(t *testing.T) {
	for label, task := range map[string]string{"inline task": "short task", "task by file": strings.Repeat("x\n", 9000)} {
		cmd := typedCommand(t, "/nonexistent/script.sh", task, t.TempDir()).Command
		for _, marker := range []string{"DATAWATCH_COMPLETE:", "DATAWATCH_FAILED:"} {
			if strings.Contains(cmd, marker) {
				t.Errorf("%s: the typed command holds %q: %s", label, marker, cmd)
			}
		}
	}
}
