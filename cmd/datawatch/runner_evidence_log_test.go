package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestNewRunnerEvidenceFallsBackToLog(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("TMUX_TMPDIR", "")
	logPath := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(logPath, []byte("working\n\x1b[31mfailed\x1b[0m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := newRunnerEvidence("", "no-such-session", logPath, "node-a", "model-x")
	if ev.Node != "node-a" || ev.Model != "model-x" {
		t.Fatalf("node/model not carried: %+v", ev)
	}
	if len(ev.LastOutput) != 2 || ev.LastOutput[1] != "failed" {
		t.Fatalf("log output not used: %+v", ev.LastOutput)
	}
}

func TestNewRunnerEvidencePrefersPane(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("TMUX_TMPDIR", "")
	logPath := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(logPath, []byte("from log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := newRunnerEvidence("from pane\n", "no-such-session", logPath, "n", "m")
	if len(ev.LastOutput) != 1 || ev.LastOutput[0] != "from pane" {
		t.Fatalf("pane not preferred: %+v", ev.LastOutput)
	}
}

// unsetTmuxEnv removes TMUX, TMUX_PANE and TMUX_TMPDIR for the test (really
// unset, not set empty) and restores the previous values afterwards, so
// nothing the test reaches can touch the operator's tmux server.
func unsetTmuxEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"TMUX", "TMUX_PANE", "TMUX_TMPDIR"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { _ = os.Setenv(k, v) })
		}
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPaneGoneEvidenceFromLogNamesExitNodeModel covers a pane that is gone or
// errored (an empty capture) while output.log holds DATAWATCH_PI_FAILED
// lines: the evidence carries the last exit status, node and model, and the
// stall summary names them while still matching ErrWorkerStalled.
func TestPaneGoneEvidenceFromLogNamesExitNodeModel(t *testing.T) {
	unsetTmuxEnv(t)
	logPath := filepath.Join(t.TempDir(), "output.log")
	content := "working\nDATAWATCH_PI_FAILED: pi exited with status 1 (model m)\nretry\nDATAWATCH_PI_FAILED: pi exited with status 2 (model m)\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Empty pane stands in for a pane that is gone or errored.
	ev := newRunnerEvidence("  \n", "no-such-session-pane-gone", logPath, "gpu-b", "qwen-x")
	if ev.ExitStatus == nil || *ev.ExitStatus != 2 {
		t.Fatalf("exit status = %v, want 2", ev.ExitStatus)
	}
	if ev.Node != "gpu-b" || ev.Model != "qwen-x" {
		t.Fatalf("node/model not carried: %+v", ev)
	}
	if n := len(ev.LastOutput); n == 0 || ev.LastOutput[n-1] != "DATAWATCH_PI_FAILED: pi exited with status 2 (model m)" {
		t.Fatalf("log tail not captured: %+v", ev.LastOutput)
	}
	base := autonomouspkg.ExitSilenceError("runner gone")
	err := withRunnerExitStatus(base, ev)
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Fatalf("lost ErrWorkerStalled: %v", err)
	}
	if !strings.HasPrefix(err.Error(), base.Error()) {
		t.Fatalf("summary %q does not start with the original error %q", err.Error(), base.Error())
	}
	for _, want := range []string{"pi exited with status 2", "node gpu-b", "model qwen-x"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("summary %q missing %q", err.Error(), want)
		}
	}
}
