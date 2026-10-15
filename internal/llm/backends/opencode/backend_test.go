package opencode

import (
	"os"
	"strings"
	"testing"
)

// 2026-10-15 — operator-reported: the PWA session view for an opencode
// task showed screen after screen of the task spec's own text with no
// visible sign of real work, because the whole task was embedded inline
// in the typed command and echoed by the pty. writeTaskFile moves the
// content to a file so the typed line stays short.
func TestWriteTaskFile(t *testing.T) {
	task := "line one\nline two with 'quotes' and \"stuff\"\nCHECKPOINT PROTOCOL etc."
	path, err := writeTaskFile("johnnyjohnny-a400", task)
	if err != nil {
		t.Fatalf("writeTaskFile: %v", err)
	}
	defer os.Remove(path) //nolint:errcheck

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != task {
		t.Errorf("file content mismatch: got %q, want %q", got, task)
	}
	if !strings.Contains(path, "johnnyjohnny-a400") {
		t.Errorf("path %q should carry the tmux session name for traceability", path)
	}
}

// buildRunCmd must produce a short typed line (not the whole task spec
// inline) and must still reference the real content via command
// substitution, so opencode receives the exact same text it always did.
func TestBuildRunCmd_KeepsTypedLineShort(t *testing.T) {
	b := &Backend{binary: "opencode"}
	bigTask := strings.Repeat("this is a long task spec line.\n", 500) // ~16KB
	cmd := b.buildRunCmd("/tmp/proj", bigTask, "johnnyjohnny-xyz1")

	if len(cmd) > 2000 {
		t.Errorf("typed command is %d bytes — expected the big task text to live in a file, not be embedded inline (len should stay small regardless of task size)", len(cmd))
	}
	if !strings.Contains(cmd, "opencode run \"$(cat '") {
		t.Errorf("expected command substitution form, got: %s", cmd)
	}
	if !strings.Contains(cmd, "rm -f '") {
		t.Errorf("expected scratch file cleanup, got: %s", cmd)
	}
	if strings.Contains(cmd, "this is a long task spec line") {
		t.Errorf("task text leaked into the typed command — it must only live in the scratch file")
	}
}

func TestBuildRunCmd_EmptyTaskUnchanged(t *testing.T) {
	b := &Backend{binary: "opencode"}
	cmd := b.buildRunCmd("/tmp/proj", "", "johnnyjohnny-xyz1")
	want := "cd '/tmp/proj' && opencode"
	if cmd != want {
		t.Errorf("buildRunCmd empty task = %q, want %q", cmd, want)
	}
}
