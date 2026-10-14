package session

// Hidden behavioural check for the model-tiering eval "analysis" cell.
// verify/analysis.sh copies this file into the cell's internal/session/ as
// zz_eval_hidden_test.go, runs it, and removes it again. It is independent of
// whatever tests the worker wrote, so a cell cannot pass by weakening its own
// tests. Lives under testdata/, so the go tool never builds it in place.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func evalHiddenGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

func evalHiddenSession(id string) *Session {
	now := time.Now()
	return &Session{ID: id, FullID: "evalhost-" + id, Task: "eval hidden", Hostname: "evalhost",
		State: StateRunning, CreatedAt: now, UpdatedAt: now}
}

func evalHiddenGrow(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Repeat("y", n)); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestEvalHidden_NewTrackerSafeAndOutputLogUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tr, err := NewTracker(t.TempDir(), evalHiddenSession("e1e1"))
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	dir := tr.SessionDir()
	if got := evalHiddenGit(t, dir, "config", "--get", "gc.auto"); got != "0" {
		t.Errorf("gc.auto = %q, want 0", got)
	}
	if got := evalHiddenGit(t, dir, "config", "--get", "gc.autoDetach"); got != "false" {
		t.Errorf("gc.autoDetach = %q, want false", got)
	}
	for i := 0; i < 5; i++ {
		evalHiddenGrow(t, tr.OutputLogPath(), 64*1024)
		if err := tr.RecordStateChange(StateRunning, StateWaitingInput); err != nil {
			t.Fatalf("RecordStateChange: %v", err)
		}
	}
	if got := evalHiddenGit(t, dir, "log", "--all", "--oneline", "--", "output.log"); got != "" {
		t.Errorf("output.log is in history:\n%s", got)
	}
	if got := evalHiddenGit(t, dir, "log", "--all", "--oneline", "--", "timeline.md"); got == "" {
		t.Errorf("timeline.md is not in history")
	}
	if st, err := os.Stat(tr.OutputLogPath()); err != nil || st.Size() < 5*64*1024 {
		t.Errorf("output.log missing or truncated on disk: %v", err)
	}
}

func TestEvalHidden_ResumeUntracksLegacyOutputLog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dataDir := t.TempDir()
	sess := evalHiddenSession("e2e2")
	dir := filepath.Join(dataDir, "sessions", sess.FullID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "e@x"},
		{"config", "user.name", "e"},
	} {
		evalHiddenGit(t, dir, args...)
	}
	evalHiddenGrow(t, filepath.Join(dir, "output.log"), 4096)
	evalHiddenGrow(t, filepath.Join(dir, "timeline.md"), 10)
	evalHiddenGit(t, dir, "add", "-A")
	evalHiddenGit(t, dir, "commit", "-q", "-m", "legacy")
	if got := evalHiddenGit(t, dir, "ls-files", "output.log"); got != "output.log" {
		t.Fatalf("legacy setup failed: output.log not tracked (%q)", got)
	}

	tr := ResumeTracker(dataDir, sess)
	if err := tr.RecordStateChange(StateRunning, StateWaitingInput); err != nil {
		t.Fatalf("RecordStateChange: %v", err)
	}
	if got := evalHiddenGit(t, dir, "ls-files", "output.log"); got != "" {
		t.Errorf("output.log still tracked after resume + record")
	}
	if got := evalHiddenGit(t, dir, "config", "--get", "gc.auto"); got != "0" {
		t.Errorf("gc.auto = %q after resume, want 0", got)
	}
	ex, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if !strings.Contains(string(ex), "output.log") {
		t.Errorf(".git/info/exclude lacks output.log")
	}
	if _, err := os.Stat(filepath.Join(dir, "output.log")); err != nil {
		t.Errorf("output.log removed from disk: %v", err)
	}
}
