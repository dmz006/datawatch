package session

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveOneShotScratchRemovesAllPiScratchPrefixes verifies that the
// cleanup path handles all three pi scratch file families (task, out, status),
// derived from the sanitized session name. The scratch dir lives inside the
// repo so the test never writes outside the project directory.
func TestRemoveOneShotScratchRemovesAllPiScratchPrefixes(t *testing.T) {
	// Keep all scratch writes inside the project directory, never in /tmp.
	scratchRoot, err := os.MkdirTemp(".", "oneshot-scratch-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(scratchRoot) }()
	t.Setenv("TMPDIR", scratchRoot)
	scratch := os.TempDir()

	const session = "cs-testhost-gate1"
	taskFile := filepath.Join(scratch, "datawatch-pi-task-"+session+"-111.txt")
	outFile := filepath.Join(scratch, "datawatch-pi-out-"+session+"-222")
	statusFile := filepath.Join(scratch, "datawatch-pi-status-"+session+"-333")
	unrelated := filepath.Join(scratch, "datawatch-pi-task-other-session-999.txt")
	for _, f := range []string{taskFile, outFile, statusFile, unrelated} {
		if err := os.WriteFile(f, []byte("scratch"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", f, err)
		}
	}

	removeOneShotScratch(session)

	for _, f := range []string{taskFile, outFile, statusFile} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, stat err=%v", f, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated file should be untouched, stat err=%v", err)
	}

	// A nonexistent session must be a no-op, not a panic.
	removeOneShotScratch("cs-does-not-exist-1")

	// An empty session name must be a no-op.
	removeOneShotScratch("")
}

// TestRemoveOneShotScratchSanitizesSessionName checks that the cleanup path
// sanitizes the session name the same way the pi backend does when reserving
// scratch files.
func TestRemoveOneShotScratchSanitizesSessionName(t *testing.T) {
	scratchRoot, err := os.MkdirTemp(".", "oneshot-scratch-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(scratchRoot) }()
	t.Setenv("TMPDIR", scratchRoot)
	scratch := os.TempDir()

	richName := "cs@prd-1ee80001/task 42"
	sanitized := piScratchNamePart(richName)
	if sanitized != "cs-prd-1ee80001-task-42" {
		t.Fatalf("piScratchNamePart(%q) = %q", richName, sanitized)
	}

	f := filepath.Join(scratch, "datawatch-pi-task-"+sanitized+"-7.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	removeOneShotScratch(richName)

	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("expected sanitized scratch file %s removed, stat err=%v", f, err)
	}
}
