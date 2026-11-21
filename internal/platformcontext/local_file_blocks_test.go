package platformcontext

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testCrossBlock = crossBegin + "\n# Cross\nbody\n" + crossEnd

// The memory and cross-session blocks sit beside the platform context and the
// RTK block in the untracked file, each replaced in place on a rewrite, with
// operator text outside the markers kept.
func TestLocalFile_MemoryAndCrossSessionBlocks_UpsertAndRemove(t *testing.T) {
	dir := localRepo(t)
	path := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(path, []byte("operator text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w, err := WriteMemoryLocalFile(dir, "# Memory & Knowledge (datawatch)\n\nuse memory\n"); err != nil || !w {
		t.Fatalf("memory write: w=%v err=%v", w, err)
	}
	if w, err := WriteCrossSessionLocalFile(dir, testCrossBlock); err != nil || !w {
		t.Fatalf("cross write: w=%v err=%v", w, err)
	}
	if _, err := WritePlatformContextLocalFile(dir, "platform body", 1); err != nil {
		t.Fatal(err)
	}
	got := localRead(t, path)
	for _, want := range []string{"operator text", memoryBegin, memoryEnd, "use memory", crossBegin, crossEnd, "platform body"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}

	// Rewriting with changed text replaces in place and does not duplicate.
	if w, err := WriteMemoryLocalFile(dir, "# Memory & Knowledge (datawatch)\n\nnew memory text\n"); err != nil || !w {
		t.Fatalf("memory rewrite: w=%v err=%v", w, err)
	}
	got = localRead(t, path)
	if strings.Count(got, memoryBegin) != 1 || strings.Contains(got, "use memory") || !strings.Contains(got, "new memory text") {
		t.Fatalf("memory block not replaced in place:\n%s", got)
	}
	// An identical rewrite changes nothing.
	if w, err := WriteMemoryLocalFile(dir, "# Memory & Knowledge (datawatch)\n\nnew memory text\n"); err != nil || w {
		t.Fatalf("identical rewrite reported w=%v err=%v", w, err)
	}
	if w, err := WriteCrossSessionLocalFile(dir, testCrossBlock); err != nil || w {
		t.Fatalf("identical cross rewrite reported w=%v err=%v", w, err)
	}

	// The file is excluded from git.
	if out, _ := gitStatusOutput(dir); strings.Contains(out, "CLAUDE.local.md") {
		t.Errorf("CLAUDE.local.md shows in git status:\n%s", out)
	}

	// Removing one block leaves the others and the operator text.
	if r, err := RemoveMemoryLocalFile(dir); err != nil || !r {
		t.Fatalf("memory remove: r=%v err=%v", r, err)
	}
	got = localRead(t, path)
	if strings.Contains(got, memoryBegin) || !strings.Contains(got, crossBegin) || !strings.Contains(got, "operator text") {
		t.Fatalf("after memory removal:\n%s", got)
	}
	if r, err := RemoveCrossSessionLocalFile(dir); err != nil || !r {
		t.Fatalf("cross remove: r=%v err=%v", r, err)
	}
	if got = localRead(t, path); strings.Contains(got, crossBegin) || !strings.Contains(got, "platform body") {
		t.Fatalf("after cross removal:\n%s", got)
	}
	// Removing an absent block is a no-op.
	if r, err := RemoveMemoryLocalFile(dir); err != nil || r {
		t.Fatalf("second memory remove: r=%v err=%v", r, err)
	}
}

// Removing the last block deletes the file; a missing file is not an error.
func TestLocalFile_MemoryBlockOnly_RemoveDeletesFile(t *testing.T) {
	dir := t.TempDir()
	if r, err := RemoveCrossSessionLocalFile(dir); err != nil || r {
		t.Fatalf("remove on missing file: r=%v err=%v", r, err)
	}
	if _, err := WriteMemoryLocalFile(dir, "memory"); err != nil {
		t.Fatal(err)
	}
	if r, err := RemoveMemoryLocalFile(dir); err != nil || !r {
		t.Fatalf("r=%v err=%v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.local.md")); !os.IsNotExist(err) {
		t.Errorf("file should be deleted, err=%v", err)
	}
}

// A tracked CLAUDE.local.md is refused for both new writers.
func TestLocalFile_MemoryAndCrossSession_RefuseTrackedFile(t *testing.T) {
	dir := localRepo(t)
	path := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(path, []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	localGit(t, dir, "add", "CLAUDE.local.md")
	localGit(t, dir, "commit", "-q", "-m", "x")
	if _, err := WriteMemoryLocalFile(dir, "m"); err == nil {
		t.Error("memory write into a tracked file did not fail")
	}
	if _, err := WriteCrossSessionLocalFile(dir, testCrossBlock); err == nil {
		t.Error("cross write into a tracked file did not fail")
	}
	if got := localRead(t, path); got != "tracked\n" {
		t.Errorf("tracked file changed: %q", got)
	}
}

// The strip helper recognises the memory block the local file carries.
func TestStripDaemonSections_RemovesMemoryMarkerBlock(t *testing.T) {
	in := "keep me\n\n" + memoryBegin + "\n# Memory & Knowledge (datawatch)\n\nbody\n" + memoryEnd + "\n"
	if got := StripDaemonSections(in); got != "keep me" {
		t.Errorf("got %q", got)
	}
}

func gitStatusOutput(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=all").Output()
	return string(out), err
}
