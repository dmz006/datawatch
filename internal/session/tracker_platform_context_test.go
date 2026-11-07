package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

func TestWriteSessionGuardrails_PlatformContext_MergeIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	projectDir := t.TempDir()
	target := filepath.Join(projectDir, "CLAUDE.md")
	userText := "# My project\n\nUser rule: always be kind.\n"
	if err := os.WriteFile(target, []byte(userText), 0644); err != nil {
		t.Fatal(err)
	}
	tr, sess, err := newGuardrailsTestSession(dataDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	opts := GuardrailsOptions{PlatformContextEnabled: true}
	if err := tr.WriteSessionGuardrails("", sess, opts); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(target)
	if n := strings.Count(string(first), "<!-- datawatch:platform-context v"); n != 1 {
		t.Fatalf("section count = %d, want 1:\n%s", n, first)
	}
	if !strings.Contains(string(first), userText) {
		t.Fatalf("user text not preserved:\n%s", first)
	}
	if !strings.Contains(string(first), strings.TrimSpace(platformcontext.PlatformContext)) {
		t.Fatalf("platform context text missing")
	}
	if err := tr.WriteSessionGuardrails("", sess, opts); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(target)
	if string(first) != string(second) {
		t.Fatalf("second write changed the file:\n--first--\n%s\n--second--\n%s", first, second)
	}
}

func TestWriteSessionGuardrails_PlatformContext_CreatePath(t *testing.T) {
	projectDir := t.TempDir()
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(projectDir, "CLAUDE.md"))
	if n := strings.Count(string(b), "<!-- datawatch:platform-context v"); n != 1 {
		t.Fatalf("section count = %d, want 1", n)
	}
	// Second write goes through the merge path and must not duplicate.
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(filepath.Join(projectDir, "CLAUDE.md"))
	if string(b) != string(b2) {
		t.Fatalf("file changed on second write")
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
	b, _ := os.ReadFile(target)
	if strings.Contains(string(b), "datawatch:platform-context") {
		t.Fatalf("section added while disabled")
	}
}

func TestWriteSessionGuardrails_PlatformContext_DisabledRemovesSectionOnly(t *testing.T) {
	projectDir := t.TempDir()
	target := filepath.Join(projectDir, "CLAUDE.md")
	userText := "# My project\n\nUser rule: always be kind.\n"
	if err := os.WriteFile(target, []byte(userText), 0644); err != nil {
		t.Fatal(err)
	}
	tr, sess, err := newGuardrailsTestSession(t.TempDir(), projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
		t.Fatal(err)
	}
	withSection, _ := os.ReadFile(target)
	if !strings.Contains(string(withSection), "datawatch:platform-context") {
		t.Fatalf("setup: section missing")
	}
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(target)
	if strings.Contains(string(after), "datawatch:platform-context") {
		t.Fatalf("section not removed:\n%s", after)
	}
	if strings.Contains(string(after), strings.TrimSpace(platformcontext.PlatformContext)) {
		t.Fatalf("section body not removed")
	}
	if !strings.Contains(string(after), userText) {
		t.Fatalf("user text damaged:\n%s", after)
	}
}

func TestWriteSessionGuardrails_PlatformContext_DisabledNoSectionNotRewritten(t *testing.T) {
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
	before, _ := os.ReadFile(target)
	if err := tr.WriteSessionGuardrails("", sess, GuardrailsOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(target)
	fi, _ := os.Stat(target)
	if string(before) != string(after) {
		t.Fatalf("content changed")
	}
	if !fi.ModTime().Equal(past) {
		t.Fatalf("file was rewritten (mtime %v != %v)", fi.ModTime(), past)
	}
}
