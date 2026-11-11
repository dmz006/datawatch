package main

import (
	"os"
	"path/filepath"
	"testing"
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
