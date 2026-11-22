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

func TestPaneGoneEvidenceFromLogNamesExitNodeModel(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
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
	base := autonomouspkg.ExitSilenceError("runner gone")
	err := withRunnerExitStatus(base, ev)
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Fatalf("lost ErrWorkerStalled: %v", err)
	}
	for _, want := range []string{"pi exited with status 2", "node gpu-b", "model qwen-x"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("summary %q missing %q", err.Error(), want)
		}
	}
}
