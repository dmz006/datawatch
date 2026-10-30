package session

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func newTrackerTestSession(id string) *Session {
	now := time.Now()
	return &Session{
		ID:        id,
		FullID:    "testhost-" + id,
		Task:      "tracker hygiene test",
		Hostname:  "testhost",
		State:     StateRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestNewTracker_SetsSafeGCConfig(t *testing.T) {
	requireGit(t)
	tr, err := NewTracker(t.TempDir(), newTrackerTestSession("a1b2"))
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	dir := tr.SessionDir()
	if got := gitOut(t, dir, "config", "--get", "gc.auto"); got != "0" {
		t.Errorf("gc.auto = %q, want 0", got)
	}
	if got := gitOut(t, dir, "config", "--get", "gc.autoDetach"); got != "false" {
		t.Errorf("gc.autoDetach = %q, want false", got)
	}
	if got := gitOut(t, dir, "config", "--get", "pack.windowMemory"); got != "64m" {
		t.Errorf("pack.windowMemory = %q, want 64m", got)
	}
	if got := gitOut(t, dir, "config", "--get", "pack.threads"); got != "1" {
		t.Errorf("pack.threads = %q, want 1", got)
	}
}

func TestTracker_OutputLogNotCommitted(t *testing.T) {
	requireGit(t)
	tr, err := NewTracker(t.TempDir(), newTrackerTestSession("c3d4"))
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	dir := tr.SessionDir()
	chunk := bytes.Repeat([]byte("x"), 256*1024)
	const rounds = 20
	for i := 0; i < rounds; i++ {
		f, err := os.OpenFile(tr.OutputLogPath(), os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatalf("open output.log: %v", err)
		}
		if _, err := f.Write(chunk); err != nil {
			t.Fatalf("write output.log: %v", err)
		}
		_ = f.Close()
		if i%2 == 0 {
			err = tr.RecordInputSent("input", "test")
		} else {
			err = tr.RecordStateChange(StateRunning, StateWaitingInput)
		}
		if err != nil {
			t.Fatalf("record round %d: %v", i, err)
		}
	}

	if got := gitOut(t, dir, "log", "--all", "--oneline", "--", "output.log"); got != "" {
		t.Errorf("output.log appears in history:\n%s", got)
	}
	if got := gitOut(t, dir, "ls-files", "output.log"); got != "" {
		t.Errorf("output.log is tracked: %q", got)
	}
	for _, f := range []string{"conversation.md", "timeline.md"} {
		if got := gitOut(t, dir, "log", "--all", "--oneline", "--", f); got == "" {
			t.Errorf("%s missing from history", f)
		}
	}
	fi, err := os.Stat(tr.OutputLogPath())
	if err != nil {
		t.Fatalf("output.log missing on disk: %v", err)
	}
	if want := int64(rounds * len(chunk)); fi.Size() != want {
		t.Errorf("output.log size = %d, want %d", fi.Size(), want)
	}
}

func TestResumeTracker_UntracksLegacyOutputLog(t *testing.T) {
	requireGit(t)
	dataDir := t.TempDir()
	sess := newTrackerTestSession("e5f6")
	dir := filepath.Join(dataDir, "sessions", sess.FullID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Legacy (pre-fix) repo: output.log committed, no gc config, no excludes.
	gitOut(t, dir, "init")
	gitOut(t, dir, "config", "user.email", "test@localhost")
	gitOut(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "output.log"), []byte("legacy output\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "timeline.md"), []byte("# Timeline\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", "-A")
	gitOut(t, dir, "commit", "-q", "-m", "legacy")
	if got := gitOut(t, dir, "ls-files", "output.log"); got != "output.log" {
		t.Fatalf("setup: output.log not tracked (%q)", got)
	}

	tr := ResumeTracker(dataDir, sess)
	if err := tr.RecordStateChange(StateRunning, StateWaitingInput); err != nil {
		t.Fatalf("RecordStateChange: %v", err)
	}

	if got := gitOut(t, dir, "ls-files", "output.log"); got != "" {
		t.Errorf("output.log still tracked: %q", got)
	}
	if got := gitOut(t, dir, "ls-tree", "--name-only", "HEAD", "output.log"); got != "" {
		t.Errorf("output.log still in HEAD tree: %q", got)
	}
	if got := gitOut(t, dir, "config", "--get", "gc.auto"); got != "0" {
		t.Errorf("gc.auto = %q, want 0", got)
	}
	exclude, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains("\n"+string(exclude), "\noutput.log\n") {
		t.Errorf("info/exclude lacks output.log:\n%s", exclude)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "output.log")); err != nil || string(data) != "legacy output\n" {
		t.Errorf("output.log on disk changed: %q, %v", data, err)
	}
}

func TestEnsureRepoHygiene_Idempotent(t *testing.T) {
	requireGit(t)
	tr, err := NewTracker(t.TempDir(), newTrackerTestSession("a7b8"))
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	tr.ensureRepoHygiene()
	tr.ensureRepoHygiene()

	exclude, err := os.ReadFile(filepath.Join(tr.SessionDir(), ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(string(exclude), "\n") {
		counts[strings.TrimSpace(line)]++
	}
	for _, pat := range outputLogExcludes {
		if counts[pat] != 1 {
			t.Errorf("exclude pattern %q appears %d times, want 1", pat, counts[pat])
		}
	}
}

func TestWorkerQuestionGuardrail(t *testing.T) {
	const marker = "DATAWATCH_QUESTION:"
	repoOnly := []string{"AGENT.md", "CONTEXT.md", "docs/plans", "decision-protocol", "decision protocol"}
	sess := &Session{FullID: "host-abcd", Task: "do a thing", ProjectDir: "/tmp/proj"}

	// check asserts one rendered guardrail text carries the worker question
	// bullet next to the unchanged needs-input text, with the marker only inline.
	check := func(t *testing.T, text string) {
		t.Helper()
		if !strings.Contains(text, "DATAWATCH_NEEDS_INPUT:") {
			t.Error("guardrail lost the needs-input marker")
		}
		if !strings.Contains(text, "`"+marker+"`") {
			t.Errorf("guardrail does not carry %s inline in backticks", marker)
		}
		for _, key := range []string{"question", "options", "recommendation", "default", "blocked"} {
			if !strings.Contains(text, key) {
				t.Errorf("guardrail does not name the %q key", key)
			}
		}
		for i, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimLeft(line, " \t>-*`")
			if strings.HasPrefix(trimmed, marker) {
				t.Errorf("line %d starts with the question marker: %q", i+1, line)
			}
		}
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, marker) {
				continue
			}
			for _, banned := range repoOnly {
				if strings.Contains(line, banned) {
					t.Errorf("question bullet names repository-only text %q", banned)
				}
			}
		}
	}

	t.Run("minimal", func(t *testing.T) {
		text := minimalSessionGuardrails(sess)
		check(t, text)
		for _, banned := range repoOnly {
			if strings.Contains(text, banned) {
				t.Errorf("minimal guardrail names repository-only text %q", banned)
			}
		}
		if !strings.Contains(text, "- If needing input: output DATAWATCH_NEEDS_INPUT: <question>\n") {
			t.Error("needs-input line changed")
		}
	})

	t.Run("template", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "templates", "session-CLAUDE.md"))
		if err != nil {
			t.Fatal(err)
		}
		check(t, string(data))
	})
}
