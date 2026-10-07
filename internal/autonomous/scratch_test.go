// 2026-10-07 — tests for ScratchDir/RelocateProjectFile, the fix for PRD
// scratch artifacts (.decompose-output-*.json, CHECKPOINT.md) landing
// unmanaged in a shared project_dir. See scratch.go's package doc.

package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScratchDir_CreatesAndReturnsPath(t *testing.T) {
	dataDir := t.TempDir()
	dir, err := ScratchDir(dataDir, "prd-123")
	if err != nil {
		t.Fatalf("ScratchDir: %v", err)
	}
	want := filepath.Join(dataDir, "autonomous", "scratch", "prd-123")
	if dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("ScratchDir did not create the directory: %v", err)
	}
}

func TestScratchDir_EmptyArgsError(t *testing.T) {
	if _, err := ScratchDir("", "prd-123"); err == nil {
		t.Error("empty dataDir should error")
	}
	if _, err := ScratchDir("/tmp", ""); err == nil {
		t.Error("empty prdID should error")
	}
}

func TestRelocateProjectFile_MovesContentAndDeletesSource(t *testing.T) {
	projectDir := t.TempDir()
	scratchDir := t.TempDir()
	src := filepath.Join(projectDir, "CHECKPOINT.md")
	if err := os.WriteFile(src, []byte("step 3 of 5 done"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, ok := RelocateProjectFile(projectDir, "CHECKPOINT.md", scratchDir, "task-abc.md")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if content != "step 3 of 5 done" {
		t.Errorf("got content %q", content)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file should be deleted from project_dir after relocation")
	}
	dst := filepath.Join(scratchDir, "task-abc.md")
	got, err := os.ReadFile(dst) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatalf("archived copy missing: %v", err)
	}
	if string(got) != "step 3 of 5 done" {
		t.Errorf("archived content = %q", got)
	}
}

func TestRelocateProjectFile_MissingFileIsNoop(t *testing.T) {
	projectDir := t.TempDir()
	content, ok := RelocateProjectFile(projectDir, "CHECKPOINT.md", t.TempDir(), "dest.md")
	if ok || content != "" {
		t.Errorf("missing file should return (\"\", false), got (%q, %v)", content, ok)
	}
}

func TestRelocateProjectFile_EmptyFileIsNoop(t *testing.T) {
	projectDir := t.TempDir()
	src := filepath.Join(projectDir, "CHECKPOINT.md")
	if err := os.WriteFile(src, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	content, ok := RelocateProjectFile(projectDir, "CHECKPOINT.md", t.TempDir(), "dest.md")
	if ok || content != "" {
		t.Errorf("empty file should return (\"\", false), got (%q, %v)", content, ok)
	}
	// An empty file is not a real checkpoint -- left in place, not deleted,
	// since there's nothing worth relocating.
	if _, err := os.Stat(src); err != nil {
		t.Error("empty source file should NOT be deleted (nothing was relocated)")
	}
}

func TestRelocateProjectFile_ArchiveWriteFailureStillDeletesSource(t *testing.T) {
	// scratchDir="" simulates ScratchDir() having failed upstream (e.g.
	// a permissions error building the data-dir path) -- the relocation's
	// real goal (get the file OUT of project_dir) must still happen even
	// when the archival copy can't be written.
	projectDir := t.TempDir()
	src := filepath.Join(projectDir, "CHECKPOINT.md")
	if err := os.WriteFile(src, []byte("progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, ok := RelocateProjectFile(projectDir, "CHECKPOINT.md", "", "dest.md")
	if !ok || content != "progress" {
		t.Fatalf("got (%q, %v), want (\"progress\", true)", content, ok)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should still be deleted even when scratchDir is empty")
	}
}

func TestRelocateProjectFile_EmptyProjectDirIsNoop(t *testing.T) {
	content, ok := RelocateProjectFile("", "CHECKPOINT.md", t.TempDir(), "dest.md")
	if ok || content != "" {
		t.Errorf("empty projectDir should return (\"\", false), got (%q, %v)", content, ok)
	}
}
