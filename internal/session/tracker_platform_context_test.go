package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

const pcMarker = "<!-- datawatch:platform-context v"

func pcGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func pcCommit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func pcRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteSessionGuardrails_PlatformContext_LocalFileIdempotent(t *testing.T) {
	projectDir := t.TempDir()
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	opts := GuardrailsOptions{PlatformContextEnabled: true}
	if err := tr.WriteSessionGuardrails("", sess, opts); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(projectDir, "CLAUDE.local.md")
	first := pcRead(t, local)
	if n := strings.Count(first, pcMarker); n != 1 {
		t.Fatalf("section count = %d, want 1:\n%s", n, first)
	}
	if !strings.Contains(first, strings.TrimSpace(platformcontext.PlatformContext)) {
		t.Fatalf("platform context text missing")
	}
	if err := tr.WriteSessionGuardrails("", sess, opts); err != nil {
		t.Fatal(err)
	}
	if second := pcRead(t, local); first != second {
		t.Fatalf("second write changed the file")
	}
	if !sess.PlatformContextLocalFile {
		t.Fatalf("flag false after an unchanged successful write")
	}
}

func TestWriteSessionGuardrails_PlatformContext_CreatePathLeavesTrackedFileClean(t *testing.T) {
	projectDir := t.TempDir()
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if b := pcRead(t, filepath.Join(projectDir, "CLAUDE.md")); strings.Contains(b, "datawatch:platform-context") {
		t.Fatalf("block written into the created CLAUDE.md")
	}
}

func TestWriteSessionGuardrails_PlatformContext_DisabledAddsNothing(t *testing.T) {
	projectDir := t.TempDir()
	target := filepath.Join(projectDir, "CLAUDE.md")
	os.WriteFile(target, []byte("user\n"), 0644) //nolint:errcheck
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pcRead(t, target), "datawatch:platform-context") {
		t.Fatalf("section added while disabled")
	}
	if _, err := os.Stat(filepath.Join(projectDir, "CLAUDE.local.md")); err == nil {
		t.Fatalf("local file created while disabled")
	}
	if sess.PlatformContextLocalFile {
		t.Fatalf("flag true while disabled")
	}
}

func TestWriteSessionGuardrails_PlatformContext_DisabledRemovesLocalBlock(t *testing.T) {
	projectDir := t.TempDir()
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(projectDir, "CLAUDE.local.md")
	if err := os.WriteFile(local, []byte("operator note\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pcRead(t, local), "datawatch:platform-context") {
		t.Fatalf("setup: block missing")
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}
	after := pcRead(t, local)
	if strings.Contains(after, "datawatch:platform-context") || !strings.Contains(after, "operator note") {
		t.Fatalf("block not removed cleanly:\n%s", after)
	}
}

func TestWriteSessionGuardrails_PlatformContext_DisabledNoBlockNotRewritten(t *testing.T) {
	projectDir := t.TempDir()
	target := filepath.Join(projectDir, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("user\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(target, past, past); err != nil {
		t.Fatal(err)
	}
	before := pcRead(t, target)
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(target)
	if before != pcRead(t, target) || !fi.ModTime().Equal(past) {
		t.Fatalf("file was rewritten")
	}
}

func TestSessionGuardrailsLocalLeavesCommittedFileIdentical(t *testing.T) {
	for _, name := range []string{"AGENT.md", "CLAUDE.md"} {
		t.Run(name, func(t *testing.T) {
			projectDir := pcGitRepo(t)
			target := filepath.Join(projectDir, name)
			userText := "# My project\n\nUser rule: always be kind.\n"
			if err := os.WriteFile(target, []byte(userText), 0644); err != nil {
				t.Fatal(err)
			}
			pcCommit(t, projectDir)
			tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
				t.Fatal(err)
			}
			if got := pcRead(t, target); got != userText {
				t.Fatalf("%s changed:\n%s", name, got)
			}
		})
	}
}

func TestSessionGuardrailsLocalFileHoldsBlock(t *testing.T) {
	projectDir := pcGitRepo(t)
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true, PlatformContextText: "custom body xyz"}); err != nil {
		t.Fatal(err)
	}
	b := pcRead(t, filepath.Join(projectDir, "CLAUDE.local.md"))
	if strings.Count(b, pcMarker) != 1 || !strings.Contains(b, "custom body xyz") {
		t.Fatalf("local file missing block:\n%s", b)
	}
	if out, _ := exec.Command("git", "-C", projectDir, "status", "--porcelain").Output(); strings.Contains(string(out), "CLAUDE.local.md") {
		t.Fatalf("local file not excluded from git:\n%s", out)
	}
}

func TestSessionGuardrailsLocalFlagTrueOnSuccess(t *testing.T) {
	projectDir := pcGitRepo(t)
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if !sess.PlatformContextLocalFile {
		t.Fatalf("flag false after a successful local write")
	}
}

func TestSessionGuardrailsLocalFlagFalseWhenRefused(t *testing.T) {
	projectDir := pcGitRepo(t)
	local := filepath.Join(projectDir, "CLAUDE.local.md")
	tracked := "tracked operator file\n"
	if err := os.WriteFile(local, []byte(tracked), 0644); err != nil {
		t.Fatal(err)
	}
	pcCommit(t, projectDir)
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if sess.PlatformContextLocalFile {
		t.Fatalf("flag true although the tracked local file was refused")
	}
	if got := pcRead(t, local); got != tracked {
		t.Fatalf("tracked local file modified:\n%s", got)
	}
}
